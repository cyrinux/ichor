package ichorgo

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// A share link names a screen of a cluster so that whoever holds a talosconfig for the same
// cluster opens it on their phone. It carries no credential: the cluster is a hash of its CA,
// the rest are the names the screen shows. A link only navigates, it never acts.
const (
	shareLinkVersion = "1"
	shareLinkWeb     = "https://cyrinux.github.io/ichor/open/"
	shareLinkScheme  = "ichor"
	shareLinkHost    = "open"
	shareLinkMaxLen  = 2048
)

// shareTarget is the JSON both apps exchange with BuildShareLink and ParseShareLink.
type shareTarget struct {
	Cluster   string `json:"cluster"`
	Target    string `json:"target"`
	Host      string `json:"host,omitempty"`
	Addr      string `json:"addr,omitempty"`
	Tab       string `json:"tab,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"ns,omitempty"`
	Name      string `json:"name,omitempty"`
}

// The fields each target takes; any other is dropped.
type shareFields struct{ node, tab, kind, namespaced bool }

var shareTargets = map[string]shareFields{
	"cluster":   {},
	"etcd":      {},
	"health":    {},
	"argocd":    {},
	"flux":      {},
	"node":      {node: true, tab: true},
	"workloads": {tab: true},
	"argo-app":  {namespaced: true},
	"flux-app":  {kind: true, namespaced: true},
	"workload":  {kind: true, namespaced: true},
	"pod":       {namespaced: true},
	"cronjob":   {namespaced: true},
	// The data services screen, on one system's tab when Kind names it (its catalog id).
	"data":    {kind: true},
	"checkup": {},
}

var (
	shareNodeTabs      = []string{"services", "resources", "live", "processes", "pods", "cgroups", "kube-pods"}
	shareWorkloadsTabs = []string{"workloads", "pods", "cronjobs"}
	shareWorkloadKinds = []string{"Deployment", "StatefulSet", "DaemonSet"}

	dns1123Label     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dns1123Subdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	shareHostname    = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
	shareClusterID   = regexp.MustCompile(`^[a-z]{16}$`)

	errShareLink = errors.New("not an Ichor share link")
)

// clusterID identifies a cluster across phones and context names: a hash of its CA
// certificate (the DER, so that a re-encoded PEM still matches). Letters only, like the
// context fingerprint, so masking never rewrites it.
func clusterID(ctx *clientconfig.Context) string {
	if isOmni(ctx) {
		return letterHash(omniClusterHash(ctx))
	}

	data := []byte(strings.TrimSpace(ctx.CA))
	if decoded, err := base64.StdEncoding.DecodeString(ctx.CA); err == nil {
		if block, _ := pem.Decode(decoded); block != nil {
			data = block.Bytes
		}
	}

	return letterHash(sha256.Sum256(data))
}

func letterHash(sum [sha256.Size]byte) string {
	out := make([]byte, fingerprintLength)
	for i := range out {
		out[i] = 'a' + sum[i]%26
	}

	return string(out)
}

// BuildShareLink returns the https link for a shareTarget JSON. Its names come from the
// screen: already masked in screenshot mode, so the link is not masked again (that would
// rewrite a masked address into another one, and could touch the website's own URL).
func BuildShareLink(targetJSON string) (out string, err error) {
	defer maskErr(&err)

	var t shareTarget
	if err := json.Unmarshal([]byte(targetJSON), &t); err != nil {
		return "", fmt.Errorf("invalid share target: %w", err)
	}

	t, err = t.validated()
	if err != nil {
		return "", err
	}

	// The fragment never leaves the phone: the page that receives it does not see the names.
	return shareLinkWeb + "#" + t.query().Encode(), nil
}

// ParseShareLink reads an ichor://open?… or https://…/ichor/open/#… link and returns its
// shareTarget JSON (masked like any result, so the app's later calls unmask it). Anything
// else, or any invalid value, is refused.
func ParseShareLink(link string) (out string, err error) {
	defer maskResult(&out, &err)

	t, err := parseShareLink(link)
	if err != nil {
		return "", err
	}

	return toJSON(t)
}

func parseShareLink(link string) (shareTarget, error) {
	if len(link) > shareLinkMaxLen {
		return shareTarget{}, errShareLink
	}

	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.User != nil {
		return shareTarget{}, errShareLink
	}

	var raw string

	switch {
	case u.Scheme == shareLinkScheme && u.Host == shareLinkHost && (u.Path == "" || u.Path == "/"):
		raw = u.RawQuery
	case u.Scheme == "https" && u.Scheme+"://"+u.Host+u.Path == shareLinkWeb:
		raw = u.EscapedFragment() // decoded once, by ParseQuery, like the query of ichor://
	default:
		return shareTarget{}, errShareLink
	}

	q, err := url.ParseQuery(raw)
	if err != nil || q.Get("v") != shareLinkVersion {
		return shareTarget{}, errShareLink
	}

	t := shareTarget{
		Cluster: q.Get("c"), Target: q.Get("t"), Host: q.Get("h"), Addr: q.Get("a"),
		Tab: q.Get("tab"), Kind: q.Get("k"), Namespace: q.Get("ns"), Name: q.Get("n"),
	}

	return t.validated()
}

func (t shareTarget) query() url.Values {
	q := url.Values{"v": {shareLinkVersion}, "c": {t.Cluster}, "t": {t.Target}}
	for key, value := range map[string]string{"h": t.Host, "a": t.Addr, "tab": t.Tab, "k": t.Kind, "ns": t.Namespace, "n": t.Name} {
		if value != "" {
			q.Set(key, value)
		}
	}

	return q
}

// validated checks every field the target takes and drops the others.
func (t shareTarget) validated() (shareTarget, error) {
	fields, ok := shareTargets[t.Target]
	if !ok || !shareClusterID.MatchString(t.Cluster) {
		return shareTarget{}, errShareLink
	}

	out := shareTarget{Cluster: t.Cluster, Target: t.Target}

	if fields.node {
		if !validShareHost(t.Host) && !validShareAddr(t.Addr) {
			return shareTarget{}, errShareLink
		}

		if validShareHost(t.Host) {
			out.Host = t.Host
		}

		if validShareAddr(t.Addr) {
			out.Addr = t.Addr
		}
	}

	if fields.tab && t.Tab != "" {
		tabs := shareNodeTabs
		if t.Target == "workloads" {
			tabs = shareWorkloadsTabs
		}

		if !slices.Contains(tabs, t.Tab) {
			return shareTarget{}, errShareLink
		}

		out.Tab = t.Tab
	}

	if fields.kind && (t.Kind != "" || t.Target != "data") {
		if !validShareKind(t.Target, t.Kind) {
			return shareTarget{}, errShareLink
		}

		out.Kind = t.Kind
	}

	if fields.namespaced {
		if len(t.Namespace) > 63 || !dns1123Label.MatchString(t.Namespace) ||
			len(t.Name) > 253 || !dns1123Subdomain.MatchString(t.Name) {
			return shareTarget{}, errShareLink
		}

		out.Namespace, out.Name = t.Namespace, t.Name
	}

	return out, nil
}

func validShareKind(target, kind string) bool {
	switch target {
	case "workload":
		return slices.Contains(shareWorkloadKinds, kind)
	case "data":
		// A catalog id: an app that does not know it opens the first tab.
		return len(kind) <= 63 && dns1123Label.MatchString(kind)
	}

	_, ok := fluxKinds[kind]

	return ok && kind != "HelmChart"
}

func validShareHost(host string) bool {
	return len(host) <= 253 && shareHostname.MatchString(host)
}

func validShareAddr(addr string) bool {
	return addr != "" && (net.ParseIP(addr) != nil || validShareHost(addr))
}
