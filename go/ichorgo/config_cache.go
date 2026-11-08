package ichorgo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// Every exported call receives the whole stored config as a string. Parsing it (YAML, the
// duplicate-context pass, the PEMs of every context) costs milliseconds with many contexts,
// so each distinct text is hashed and parsed once, then served from a small LRU. A changed
// text is a new entry: nothing to invalidate. The cached values are shared between calls
// and goroutines: callers read them, never change them (resolveContext hands out a copy).

// configCacheSize bounds the texts kept: the stored talosconfig, the stored kubeconfig and
// a text being imported, with room to spare.
const configCacheSize = 4

// parsedConfig is one config text and what is derived from it, each part computed on
// first use.
type parsedConfig struct {
	// text is the input as first received (not a copy), which the parts are computed from.
	text string
	// last is the latest copy of text looked up (guarded by configCache.mu): the helpers of
	// one call all get the same string, which compares equal in constant time.
	last string
	// hash is the hex SHA-256 of text, the only pass over it.
	hash string

	kubeOnce sync.Once
	isKube   bool

	demoOnce sync.Once
	hasDemo  bool // the text names demoEndpoint

	parseOnce    sync.Once
	cfg          *clientconfig.Config // dedupeContexts applied; nil when err is set
	err          error                // loadConfig's error for this text
	rawCfg       *clientconfig.Config // parseTalosconfig's result, for learnConfig
	rawErr       error
	names        []string            // sorted context names
	fingerprints []string            // contextFingerprint of every context, in names order
	demo         map[string]struct{} // contexts whose endpoints include demoEndpoint

	summaryOnce  sync.Once
	summaryJSON  string // ParseConfig's output
	summaryCount int
	summaryErr   error
}

type configCache struct {
	mu      sync.Mutex
	entries []*parsedConfig // most recently used first
}

var configs = &configCache{}

// configParses counts talosconfig parses, for the tests.
var configParses atomic.Int64

// get returns the entry of text, adding it (hashed, not yet parsed) when missing.
func (c *configCache) get(text string) *parsedConfig {
	if p := c.peek(text); p != nil {
		return p
	}

	hash := contentHash(text)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Another call may have added it meanwhile.
	for i, p := range c.entries {
		if p.hash == hash {
			c.touchLocked(i)

			return p
		}
	}

	p := &parsedConfig{text: text, last: text, hash: hash}
	c.entries = slices.Insert(c.entries, 0, p)

	if len(c.entries) > configCacheSize {
		c.entries[configCacheSize] = nil
		c.entries = c.entries[:configCacheSize]
	}

	return p
}

// reset forgets every parsed config: what a parse derives (the fingerprints) changed.
func (c *configCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = nil
}

// contentHash is the key of a config text: the hex SHA-256 of its bytes. ConfigDigest hands
// the same value to the apps.
func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))

	return hex.EncodeToString(sum[:])
}

// peek returns the entry of text if it is cached. A new copy of a cached text (each call
// gets one from gomobile) compares as a memcmp, far cheaper than a hash; it then becomes
// the entry's last, so the other helpers of the call compare in constant time (the same
// string: same pointer).
func (c *configCache) peek(text string) *parsedConfig {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, p := range c.entries {
		if p.last == text {
			p.last = text
			c.touchLocked(i)

			return p
		}
	}

	return nil
}

func (c *configCache) touchLocked(i int) {
	if i == 0 {
		return
	}

	p := c.entries[i]
	copy(c.entries[1:i+1], c.entries[:i])
	c.entries[0] = p
}

// kube reports whether the text is a kubeconfig (isKubeconfig, computed once).
func (p *parsedConfig) kube() bool {
	p.kubeOnce.Do(func() { p.isKube = kubeconfigShaped(p.text) })

	return p.isKube
}

// demoText reports whether the text names demoEndpoint (searched once).
func (p *parsedConfig) demoText() bool {
	p.demoOnce.Do(func() { p.hasDemo = strings.Contains(p.text, demoEndpoint) })

	return p.hasDemo
}

// mentionsDemo is strings.Contains(text, demoEndpoint), answered by the cache for a text
// it holds: the search scans the whole text (PEMs included) on every call otherwise.
func mentionsDemo(text string) bool {
	if p := configs.peek(text); p != nil {
		return p.demoText()
	}

	return strings.Contains(text, demoEndpoint)
}

// parsed is loadConfig's result for the text, parsed on first use.
func (p *parsedConfig) parsed() (*clientconfig.Config, error) {
	p.parseOnce.Do(p.parse)

	return p.cfg, p.err
}

// talosconfig is parseTalosconfig's result for the text, which learnConfig reads.
func (p *parsedConfig) talosconfig() (*clientconfig.Config, error) {
	p.parseOnce.Do(p.parse)

	return p.rawCfg, p.rawErr
}

func (p *parsedConfig) parse() {
	if p.kube() {
		p.rawErr = errTalosUnavailable
	} else {
		configParses.Add(1)
		p.rawCfg, p.rawErr = parseTalosconfig(p.text)
	}

	p.err = loadConfigError(p.text, p.kube(), p.rawCfg, p.rawErr)
	if p.err != nil {
		return
	}

	p.cfg = p.rawCfg
	p.names = sortedContextNames(p.cfg)
	p.fingerprints = make([]string, 0, len(p.names))
	p.demo = map[string]struct{}{}

	for _, name := range p.names {
		ctx := p.cfg.Contexts[name]
		p.fingerprints = append(p.fingerprints, contextFingerprint(name, ctx))

		if slices.Contains(ctx.Endpoints, demoEndpoint) {
			p.demo[name] = struct{}{}
		}
	}
}

// loadConfigError is loadConfig's verdict on a text and its parse.
func loadConfigError(text string, isKube bool, cfg *clientconfig.Config, err error) error {
	switch {
	case strings.TrimSpace(text) == "":
		return errors.New("talosconfig is empty")
	case isKube:
		return errTalosUnavailable
	case err != nil:
		return fmt.Errorf("invalid talosconfig YAML: %w", err)
	case len(cfg.Contexts) == 0:
		return errors.New("talosconfig has no contexts")
	}

	return nil
}

// defaultName is defaultContextName of the parsed config.
func (p *parsedConfig) defaultName() string {
	if _, ok := p.cfg.Contexts[p.cfg.Context]; ok {
		return p.cfg.Context
	}

	return p.names[0]
}

// isDemo reports whether the named context (the current one for "") is a Talos demo.
func (p *parsedConfig) isDemo(name string) bool {
	if _, err := p.parsed(); err != nil {
		return false
	}

	if name == "" {
		name = p.defaultName()
	}

	_, ok := p.demo[name]

	return ok
}

// summary is ParseConfig's output for the text, built once.
func (p *parsedConfig) summary() (string, int, error) {
	p.summaryOnce.Do(func() { p.summaryJSON, p.summaryCount, p.summaryErr = p.buildSummary() })

	return p.summaryJSON, p.summaryCount, p.summaryErr
}

func (p *parsedConfig) buildSummary() (string, int, error) {
	cfg, err := p.parsed()
	if err != nil {
		return "", 0, err
	}

	summary := configSummary{Contexts: make([]contextSummary, 0, len(p.names))}

	for _, name := range p.names {
		s, err := summarizeContext(name, cfg.Contexts[name])
		if err != nil {
			return "", 0, fmt.Errorf("context %q: %w", name, err)
		}

		summary.Contexts = append(summary.Contexts, s)
	}

	summary.Current = p.defaultName()

	out, err := toJSON(summary)

	return out, len(summary.Contexts), err
}

// cloneContext is a copy of ctx a caller may change without touching the cached one.
func cloneContext(ctx *clientconfig.Context) *clientconfig.Context {
	out := *ctx
	out.Endpoints = slices.Clone(ctx.Endpoints)
	out.Nodes = slices.Clone(ctx.Nodes)

	if ctx.Auth.Basic != nil {
		basic := *ctx.Auth.Basic
		out.Auth.Basic = &basic
	}

	if ctx.Auth.SideroV1 != nil {
		siderov1 := *ctx.Auth.SideroV1
		out.Auth.SideroV1 = &siderov1
	}

	return &out
}
