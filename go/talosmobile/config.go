package talosmobile

import (
	"crypto/x509"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// configSummary is what the UI may know about a talosconfig: no key material.
type configSummary struct {
	Current  string           `json:"current"`
	Contexts []contextSummary `json:"contexts"`
}

type contextSummary struct {
	Name         string   `json:"name"`
	Endpoints    []string `json:"endpoints"`
	Nodes        []string `json:"nodes"`
	Roles        []string `json:"roles"`
	CertNotAfter int64    `json:"certNotAfter"`
}

// ParseConfig validates a talosconfig YAML and returns a JSON configSummary.
func ParseConfig(configYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	privacy.learnConfig(configYAML)

	cfg, err := loadConfig(configYAML)
	if err != nil {
		return "", err
	}

	names := sortedContextNames(cfg)
	summary := configSummary{Contexts: make([]contextSummary, 0, len(names))}

	for _, name := range names {
		s, err := summarizeContext(name, cfg.Contexts[name])
		if err != nil {
			return "", fmt.Errorf("context %q: %w", name, err)
		}

		summary.Contexts = append(summary.Contexts, s)
	}

	summary.Current = defaultContextName(cfg)

	return toJSON(summary)
}

func loadConfig(configYAML string) (*clientconfig.Config, error) {
	if strings.TrimSpace(configYAML) == "" {
		return nil, errors.New("talosconfig is empty")
	}

	cfg, err := clientconfig.FromString(configYAML)
	if err != nil {
		return nil, fmt.Errorf("invalid talosconfig YAML: %w", err)
	}

	if len(cfg.Contexts) == 0 {
		return nil, errors.New("talosconfig has no contexts")
	}

	return cfg, nil
}

func summarizeContext(name string, ctx *clientconfig.Context) (contextSummary, error) {
	if len(ctx.Endpoints) == 0 {
		return contextSummary{}, errors.New("no endpoints defined")
	}

	tlsCert, err := client.CertificateFromConfigContext(ctx)
	if err != nil {
		return contextSummary{}, fmt.Errorf("invalid client certificate: %w", err)
	}

	if tlsCert == nil || len(tlsCert.Certificate) == 0 {
		return contextSummary{}, errors.New("no client certificate (crt/key) defined")
	}

	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return contextSummary{}, fmt.Errorf("invalid client certificate: %w", err)
	}

	return contextSummary{
		Name:         name,
		Endpoints:    slices.Clone(ctx.Endpoints),
		Nodes:        slices.Clone(ctx.Nodes),
		Roles:        slices.Clone(leaf.Subject.Organization),
		CertNotAfter: leaf.NotAfter.Unix(),
	}, nil
}

// resolveContext returns the named context, or the config's current one when name is empty.
func resolveContext(configYAML, name string) (string, *clientconfig.Context, error) {
	cfg, err := loadConfig(configYAML)
	if err != nil {
		return "", nil, err
	}

	if name == "" {
		name = defaultContextName(cfg)
	}

	ctx, ok := cfg.Contexts[name]
	if !ok {
		return "", nil, fmt.Errorf("context %q not found in talosconfig", name)
	}

	return name, ctx, nil
}

func sortedContextNames(cfg *clientconfig.Config) []string {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// defaultContextName is the config's current context, or the first one if it is dangling.
func defaultContextName(cfg *clientconfig.Config) string {
	if _, ok := cfg.Contexts[cfg.Context]; ok {
		return cfg.Context
	}

	return sortedContextNames(cfg)[0]
}

// targetNodes mirrors talosctl: nodes default to the endpoints when unset.
// Duplicates are dropped, keeping first-seen order.
func targetNodes(ctx *clientconfig.Context) []string {
	nodes := ctx.Nodes
	if len(nodes) == 0 {
		nodes = ctx.Endpoints
	}

	seen := make(map[string]bool, len(nodes))
	out := make([]string, 0, len(nodes))

	for _, n := range nodes {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}

	return out
}
