package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

const (
	formModeDirect = "direct"
	formModeOmni   = "omni"
)

// talosForm is what the app's "enter details" form sends: a cluster reached directly (its
// endpoints, CA and client certificate) or through Omni (the instance and cluster name).
type talosForm struct {
	Mode      string   `json:"mode"`
	Name      string   `json:"name"`
	Endpoints []string `json:"endpoints,omitempty"`
	Nodes     []string `json:"nodes,omitempty"`
	CA        string   `json:"ca,omitempty"`
	Crt       string   `json:"crt,omitempty"`
	Key       string   `json:"key,omitempty"`
	OmniURL   string   `json:"omniUrl,omitempty"`
	Cluster   string   `json:"cluster,omitempty"`
	Identity  string   `json:"identity,omitempty"`
}

// BuildTalosconfig turns the form (JSON talosForm) into a one-context talosconfig, which
// the app then imports like a pasted one (ParseConfig, ImportConflicts, MergeTalosconfig).
// The certificate fields take PEM as the files hold it, or base64 PEM as talosconfigs do.
func BuildTalosconfig(formJSON string) (out string, err error) {
	// The result is a talosconfig the app imports: only the error is masked.
	defer maskErr(&err)

	var form talosForm
	if err := json.Unmarshal([]byte(formJSON), &form); err != nil {
		return "", fmt.Errorf("invalid form: %w", err)
	}

	name := strings.TrimSpace(form.Name)
	if name == "" {
		return "", errors.New("the cluster needs a name")
	}

	var ctx *clientconfig.Context

	switch form.Mode {
	case formModeDirect:
		ctx, err = directContext(form)
	case formModeOmni:
		ctx, err = omniContext(form)
	default:
		err = fmt.Errorf("unknown form mode %q", form.Mode)
	}

	if err != nil {
		return "", err
	}

	cfg := &clientconfig.Config{Context: name, Contexts: map[string]*clientconfig.Context{name: ctx}}

	raw, err := cfg.Bytes()
	if err != nil {
		return "", fmt.Errorf("write talosconfig: %w", err)
	}

	return string(raw), nil
}

func directContext(form talosForm) (*clientconfig.Context, error) {
	endpoints, err := hostList("endpoint", form.Endpoints)
	if err != nil {
		return nil, err
	}

	if len(endpoints) == 0 {
		return nil, errors.New("add at least one endpoint")
	}

	nodes, err := hostList("node", form.Nodes)
	if err != nil {
		return nil, err
	}

	ctx := &clientconfig.Context{Endpoints: endpoints, Nodes: nodes}

	for _, field := range []struct {
		label, value string
		dst          *string
	}{{"CA", form.CA, &ctx.CA}, {"client certificate", form.Crt, &ctx.Crt}, {"client key", form.Key, &ctx.Key}} {
		if *field.dst, err = base64PEM(field.label, field.value); err != nil {
			return nil, err
		}
	}

	// The library's error may quote what it failed on: keep it out of the message.
	if _, err := client.CertificateFromConfigContext(ctx); err != nil {
		return nil, errors.New("the client certificate and key do not match")
	}

	return ctx, nil
}

func omniContext(form talosForm) (*clientconfig.Context, error) {
	instance, err := omniInstanceURL(form.OmniURL)
	if err != nil {
		return nil, err
	}

	cluster := strings.TrimSpace(form.Cluster)
	if cluster == "" {
		return nil, errors.New("enter the Omni cluster name")
	}

	identity := strings.TrimSpace(form.Identity)
	if strings.ContainsFunc(identity, isSpace) {
		return nil, errors.New("the Omni identity is an email address, without spaces")
	}

	return &clientconfig.Context{
		Endpoints: []string{instance},
		Auth:      clientconfig.Auth{SideroV1: &clientconfig.SideroV1{Identity: identity}},
		Cluster:   cluster,
	}, nil
}

// omniInstanceURL is raw as https://host[:port], the form Omni's talosconfigs use.
func omniInstanceURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", errors.New("the Omni address is https://, e.g. https://acme.omni.siderolabs.io")
	}

	if strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("the Omni address is the instance only, without a path")
	}

	return "https://" + u.Host, nil
}

// hostList trims entries, drops blank ones and checks each is host or host:port.
func hostList(label string, entries []string) ([]string, error) {
	out := make([]string, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if !validHostPort(entry) {
			return nil, fmt.Errorf("%s %q is not a host or host:port", label, entry)
		}

		out = append(out, entry)
	}

	if len(out) == 0 {
		return nil, nil
	}

	return out, nil
}

func validHostPort(s string) bool {
	host := s

	if h, port, err := net.SplitHostPort(s); err == nil {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}

		host = h
	} else if strings.HasPrefix(s, "[") || strings.Count(s, ":") == 1 {
		// "[v6]" without a port, or "host:" with a bad port.
		return false
	}

	if host == "" || net.ParseIP(host) != nil {
		return host != ""
	}

	return !strings.ContainsFunc(host, func(r rune) bool {
		return isSpace(r) || strings.ContainsRune("/?#@[]:", r)
	})
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// base64PEM is value as a talosconfig stores it: base64 of the PEM text. value is either
// that already, or the PEM text itself.
func base64PEM(label, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("the %s is missing", label)
	}

	if strings.Contains(value, "-----BEGIN") {
		if block, _ := pem.Decode([]byte(value)); block == nil {
			return "", fmt.Errorf("the %s is not valid PEM", label)
		}

		return base64.StdEncoding.EncodeToString([]byte(value + "\n")), nil
	}

	compact := strings.Join(strings.Fields(value), "")

	decoded, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return "", fmt.Errorf("the %s is neither PEM nor base64 PEM", label)
	}

	if block, _ := pem.Decode(decoded); block == nil {
		return "", fmt.Errorf("the %s is neither PEM nor base64 PEM", label)
	}

	return compact, nil
}
