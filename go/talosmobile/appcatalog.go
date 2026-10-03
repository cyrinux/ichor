package talosmobile

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// The curated app catalog: which container images belong to which application. Its icons are
// bundled with the apps (scripts/sync-app-icons.py keeps the two in step).
//
//go:embed appcatalog.json
var appCatalogJSON []byte

// Every icon slug of homarr-labs/dashboard-icons, so an image the catalog does not know can still
// name an icon the app may fetch (only when the user opted in; a public slug, never the image).
//
//go:embed appicons.txt
var appIconSlugs string

type catalogApp struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"cat"`
	Icon     *string  `json:"icon"` // nil: the id; "": no icon (the app draws a monogram)
	Match    []string `json:"match"`
	Images   []string `json:"images"`
	Prefixes []string `json:"prefixes"`
	System   bool     `json:"system"`
	// A generic helper (CSI sidecar, auth proxy) joins the main app of its pod when there is one.
	Generic bool `json:"generic"`
}

// hasIcon reports whether the app has a bundled icon, named after its id.
func (a *catalogApp) hasIcon() bool { return a.Icon == nil || *a.Icon != "" }

type appCatalog struct {
	apps   []catalogApp
	byName map[string]*catalogApp // repository names (last path segment) and ids
	slugs  map[string]bool
}

var loadAppCatalog = sync.OnceValue(func() *appCatalog {
	c := &appCatalog{byName: map[string]*catalogApp{}, slugs: map[string]bool{}}
	if err := json.Unmarshal(appCatalogJSON, &c.apps); err != nil {
		panic("appcatalog.json: " + err.Error()) // embedded and checked by the tests
	}

	for i := range c.apps {
		app := &c.apps[i]

		names := app.Match
		if len(names) == 0 {
			names = []string{app.ID}
		}

		for _, name := range append(names, app.ID) {
			if _, taken := c.byName[name]; !taken {
				c.byName[name] = app
			}
		}
	}

	for _, slug := range strings.Fields(appIconSlugs) {
		c.slugs[slug] = true
	}

	return c
})

// imageRef is a parsed container image reference: docker.io/library/nginx:1.27 for "nginx:1.27".
type imageRef struct {
	Registry string
	Path     string // repository path without the registry
	Tag      string
	Digest   string
}

const defaultRegistry = "docker.io"

func parseImageRef(s string) imageRef {
	var ref imageRef

	s, ref.Digest, _ = strings.Cut(s, "@")
	if slash, colon := strings.LastIndex(s, "/"), strings.LastIndex(s, ":"); colon > slash {
		s, ref.Tag = s[:colon], s[colon+1:]
	}

	if first, rest, ok := strings.Cut(s, "/"); ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		ref.Registry, ref.Path = first, rest
	} else {
		ref.Registry, ref.Path = defaultRegistry, s
	}

	if ref.Registry == defaultRegistry && !strings.Contains(ref.Path, "/") {
		ref.Path = "library/" + ref.Path
	}

	return ref
}

// Repo is the full repository, e.g. docker.io/library/nginx.
func (r imageRef) Repo() string { return r.Registry + "/" + r.Path }

// Name is the repository's last path segment, e.g. nginx.
func (r imageRef) Name() string { return r.Path[strings.LastIndex(r.Path, "/")+1:] }

// An image only known by its digest (the CRI lost the reference) says nothing about the app.
func (r imageRef) anonymous() bool { return r.Path == "library/sha256" || r.Name() == "" }

// Helper suffixes that do not change which application an image belongs to:
// cert-manager-cainjector, longhorn-manager, immich-machine-learning… Longest first.
var helperSuffixes = []string{
	"-controller-manager", "-instance-manager", "-machine-learning", "-startupapicheck", "-acmesolver",
	"-cainjector", "-migrations", "-controller", "-operator", "-frontend", "-backend",
	"-sidecar", "-webhook", "-manager", "-daemon", "-driver", "-engine", "-plugin", "-server", "-worker",
	"-agent", "-relay", "-init", "-node", "-api", "-app", "-cli", "-nox", "-web", "-ui",
}

// helperCandidates strips one helper suffix at a time, for the caller to stop at the first known
// name: "longhorn-instance-manager" -> ["longhorn"], "cert-manager-controller" -> ["cert-manager", "cert"].
func helperCandidates(name string) []string {
	var out []string

	for {
		trimmed := name
		for _, suffix := range helperSuffixes {
			if s, ok := strings.CutSuffix(name, suffix); ok && s != "" {
				trimmed = s

				break
			}
		}

		if trimmed == name {
			return out
		}

		out = append(out, trimmed)
		name = trimmed
	}
}

// identification is what an image says about its application: a catalog app, or only an upstream
// icon slug for software the catalog does not list. Both are empty when the image is unknown.
type identification struct {
	app  *catalogApp
	slug string
}

// Base images that run as init containers or small sidecars of many apps: they say nothing about
// the app, so they join their pod's main app (or are named after their pod).
var baseImages = map[string]bool{
	"busybox": true, "alpine": true, "debian": true, "ubuntu": true, "curl": true, "socat": true,
	"kubectl": true, "bash": true, "bitnami-shell": true, "os-shell": true, "git": true, "pause": true,
	// Language runtimes and build tools run scripts and jobs; they are not the app (yet they have icons).
	"python": true, "golang": true, "go": true, "node": true, "openjdk": true, "java": true,
	"eclipse-temurin": true, "ruby": true, "php": true, "perl": true, "rust": true, "docker": true,
	"dind": true, "buildkit": true, "kaniko": true, "executor": true, "dotnet": true,
}

func (c *appCatalog) identify(ref imageRef) identification {
	if ref.anonymous() || baseImages[ref.Name()] {
		return identification{}
	}

	repo, name := ref.Repo(), ref.Name()

	for i := range c.apps {
		for _, image := range c.apps[i].Images {
			if repo == image || ref.Path == image || strings.HasSuffix(repo, "/"+image) {
				return identification{app: &c.apps[i]}
			}
		}
	}

	// A generic name (csi-attacher) loses to the vendor's own registry path (longhornio/csi-attacher).
	exact := c.byName[name]
	if exact != nil && !exact.Generic {
		return identification{app: exact}
	}

	for i := range c.apps {
		for _, prefix := range c.apps[i].Prefixes {
			if strings.HasPrefix(repo, prefix) || strings.HasPrefix(ref.Path, prefix) {
				return identification{app: &c.apps[i]}
			}
		}
	}

	if exact != nil {
		return identification{app: exact}
	}

	return c.byWord(name)
}

// byWord identifies a name (a repository, a pod's workload name) by the catalog, then by an
// upstream icon slug, with its helper suffixes stripped one at a time.
func (c *appCatalog) byWord(name string) identification {
	candidates := append([]string{name}, helperCandidates(name)...)

	for _, candidate := range candidates {
		if app := c.byName[candidate]; app != nil {
			return identification{app: app}
		}
	}

	for _, candidate := range candidates {
		if c.slugs[candidate] {
			return identification{slug: candidate}
		}
	}

	return identification{}
}

// byPod guesses the app of a pod whose images said nothing (pinned by digest, or base images only)
// from its workload name, then from its namespace.
func (c *appCatalog) byPod(namespace, pod string) identification {
	if id := c.byWord(podBaseName(pod)); isMain(id) {
		return id
	}

	if app := c.byNamespace(namespace); app != nil {
		return identification{app: app}
	}

	return identification{}
}

// byNamespace guesses the app from a namespace named after it: longhorn-system, immich,
// flux-system, or a prefixed one like team-immich.
func (c *appCatalog) byNamespace(namespace string) *catalogApp {
	candidates := []string{
		namespace,
		strings.TrimSuffix(namespace, "-system"),
		strings.TrimSuffix(namespace, "-operator"),
		strings.TrimSuffix(namespace, "-ns"),
	}
	if _, rest, ok := strings.Cut(namespace, "-"); ok {
		candidates = append(candidates, rest)
	}

	for _, candidate := range candidates {
		if app := c.byName[candidate]; app != nil && !app.Generic && !app.System {
			return app
		}
	}

	return nil
}

// Kubernetes' generated name suffixes use this alphabet (no vowels, no 0/1/3), so a trailing
// segment made only of it is a ReplicaSet hash or a random suffix rather than part of the name.
const generatedNameAlphabet = "bcdfghjklmnpqrstvwxz2456789"

// podBaseName strips the generated suffixes of a pod name: "immich-server-7d9f8c6b5-k2m8p" ->
// "immich-server", "home-assistant-0" -> "home-assistant", "cilium-8h2kd" -> "cilium".
func podBaseName(pod string) string {
	parts := strings.Split(pod, "-")
	for len(parts) > 1 && isGeneratedSuffix(parts[len(parts)-1]) {
		parts = parts[:len(parts)-1]
	}

	return strings.Join(parts, "-")
}

func isGeneratedSuffix(s string) bool {
	if s == "" {
		return false
	}

	if strings.Trim(s, "0123456789") == "" {
		return true // a StatefulSet ordinal or a CronJob's schedule time
	}

	return len(s) >= 5 && len(s) <= 10 && strings.Trim(s, generatedNameAlphabet) == ""
}

// Repository names too vague to name an app on their own: ente/web is shown as "Ente Web".
var vagueNames = map[string]bool{
	"web": true, "api": true, "app": true, "server": true, "frontend": true, "backend": true,
	"worker": true, "ui": true, "core": true, "main": true, "service": true,
}

// unknownName names an image the catalog does not know.
func unknownName(ref imageRef) string {
	name := ref.Name()
	if parts := strings.Split(ref.Path, "/"); vagueNames[name] && len(parts) > 1 {
		return displayName(parts[len(parts)-2] + "-" + name)
	}

	return displayName(name)
}

// displayName turns a slug into a readable name: "uptime-kuma" -> "Uptime Kuma".
func displayName(slug string) string {
	words := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}

	return strings.Join(words, " ")
}
