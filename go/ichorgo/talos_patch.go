package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
)

// A Talos patch file opened from a repository (the cluster's, or an app's source), tried on a
// node before it is merged: what the file is, the node's config with it applied (Talos's own
// configpatcher, as `talosctl machineconfig patch` does) for the editor's review → try chain,
// and, when the patch only sets values, the same change as field edits other nodes can be given.

const (
	talosPatchKindPatch     = "patch"     // machine/cluster sections, a v1alpha1 strategic merge patch
	talosPatchKindDocuments = "documents" // multi-doc config documents (HostnameConfig, …)
	talosPatchKindJSON      = "jsonpatch" // RFC 6902 operations
	talosPatchKindConfig    = "config"    // a whole machine config, with its secrets

	talosLegacyDoc = "v1alpha1"
	// talosOneNode ends the reasons a patch is tried on one node at a time.
	talosOneNode = ": try it on one node at a time"
)

type talosPatchDoc struct {
	Kind     string `json:"kind"` // v1alpha1 for machine/cluster sections
	Name     string `json:"name,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

type talosPatchInfo struct {
	Talos     bool            `json:"talos"`
	Kind      string          `json:"kind"`
	Documents []talosPatchDoc `json:"documents"`
	// TouchesSecrets: it sets a key the node keeps for itself (Secret names it).
	TouchesSecrets bool   `json:"touchesSecrets"`
	Secret         string `json:"secret,omitempty"`
	// Tryable: it can be applied onto a node's config; Note says why not.
	Tryable bool   `json:"tryable"`
	Note    string `json:"note,omitempty"`
}

type talosPatchDraft struct {
	Draft       string `json:"draft"`
	Same        bool   `json:"same"`
	NeedsReboot bool   `json:"needsReboot"`
}

// TalosPatchDetect tells whether content is a Talos patch that can be tried on a node:
// {"talos","kind":"patch|documents|jsonpatch|config","documents":[{"kind","name","hostname"}],
// "touchesSecrets","secret","tryable","note"}. A Kubernetes manifest, talhelper's
// talconfig.yaml or any other YAML is {"talos":false}; a whole machine config (its token and
// CA), a patch setting a secret or holding a ${...} substitution is talos but not tryable.
// Local.
func TalosPatchDetect(content string) (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(detectTalosPatch([]byte(content)))
}

func detectTalosPatch(content []byte) talosPatchInfo {
	info := talosPatchInfo{Documents: []talosPatchDoc{}}

	docs, err := yamlDocuments(content)
	if err != nil || !allTalosDocs(docs) {
		return info
	}

	info.Talos, info.Kind = true, talosPatchKindPatch

	for _, d := range docs {
		switch v := d.(type) {
		case []any:
			info.Kind = talosPatchKindJSON
		case map[string]any:
			info.Documents = append(info.Documents, patchDoc(v))
			if v["machine"] == nil && v["cluster"] == nil && info.Kind == talosPatchKindPatch {
				info.Kind = talosPatchKindDocuments
			}
		}
	}

	for _, d := range docs {
		m, _ := d.(map[string]any)
		if yamlPath(m, "machine", "token") != nil || yamlPath(m, "machine", "ca") != nil {
			if yamlPath(m, "version") != nil || yamlPath(m, "cluster", "ca") != nil {
				info.Kind = talosPatchKindConfig
			}
		}
	}

	info.Secret, info.TouchesSecrets = setsTalosSecret(docs)

	switch {
	case info.Kind == talosPatchKindConfig:
		info.Note = "a whole machine config with its secrets, not a patch: the app tries patches only"
	case info.TouchesSecrets:
		info.Note = "sets " + info.Secret + ": the node keeps its own secrets"
	case strings.Contains(string(content), "${"):
		info.Note = "holds a ${...} substitution the app does not resolve (talenv)"
	default:
		if _, err := configpatcher.LoadPatch(content); err != nil {
			info.Note = "Talos cannot load it as a patch: " + err.Error()
		} else {
			info.Tryable = true
		}
	}

	return info
}

// allTalosDocs tells every document of docs is Talos's: machine/cluster sections, a v1alpha1
// config document, or JSON patch operations on them. Any other document (a Kubernetes
// object, talconfig.yaml) makes the file something else.
func allTalosDocs(docs []any) bool {
	seen := false

	for _, d := range docs {
		switch v := d.(type) {
		case nil:
			continue
		case map[string]any:
			api, _ := v["apiVersion"].(string)
			kind, _ := v["kind"].(string)

			switch {
			case api == talosLegacyDoc && kind != "":
			case api == "" && kind == "" && (v["machine"] != nil || v["cluster"] != nil):
			default:
				return false
			}
		case []any:
			if !isTalosPatch([]any{v}) {
				return false
			}
		default:
			return false
		}

		seen = true
	}

	return seen
}

func patchDoc(m map[string]any) talosPatchDoc {
	kind, _ := m["kind"].(string)
	if kind == "" {
		kind = talosLegacyDoc
	}

	name, _ := m["name"].(string)
	doc := talosPatchDoc{Kind: kind, Name: name}

	if hosts := talosHostnames([]any{m}); len(hosts) > 0 {
		doc.Hostname = hosts[0]
	}

	return doc
}

// TalosPatchDraft applies patchYAML onto node's live config with Talos's configpatcher and
// returns the editor's draft for the review: {"draft":"<redacted patched config>","same",
// "needsReboot"}. The config is read with its secrets (os:admin), the node checks the patched
// one with an ApplyConfiguration dry run (which tells about the reboot), and only redacted
// text leaves Go: the review → try → apply chain restores the secrets from the node as for any
// draft. Refused for a patch TalosPatchDetect says is not tryable, and in privacy mode.
func TalosPatchDraft(configYAML, contextName, node, patchYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if privacy.isEnabled() {
		return "", errTalosRepoPrivacy
	}

	info := detectTalosPatch([]byte(patchYAML))
	if !info.Tryable {
		if info.Note == "" {
			return "", errors.New("this file is not a Talos patch")
		}

		return "", errors.New("this file cannot be tried: " + info.Note)
	}

	patch, err := configpatcher.LoadPatch([]byte(patchYAML))
	if err != nil {
		return "", fmt.Errorf("load the patch: %w", err)
	}

	draft := func(ctx context.Context, a configApplier) (string, error) {
		res, err := compareTalosRepo(ctx, a, []configpatcher.Patch{patch})
		if err != nil {
			return "", err
		}

		return toJSON(talosPatchDraft{Draft: res.Draft, Same: res.Same, NeedsReboot: res.NeedsReboot})
	}

	if isTalosDemoContext(configYAML, contextName) {
		text, err := demoRead("NodeMachineConfig", configYAML, contextName, node)
		if err != nil {
			return "", err
		}

		if canonical, err := canonicalConfig(text); err == nil {
			text = canonical
		}

		return draft(context.Background(), demoConfigApplier{text})
	}

	return withNodeSession(configYAML, contextName, node, configPreviewTimeout, func(ctx context.Context, s *session) (string, error) {
		return draft(ctx, nodeConfigApplier{s, node})
	})
}

// TalosPatchEdits turns patchYAML into the field edits of draftYAML that make the same change
// ({"fixable","reason","edits"}, as ConfigSnippetEdits), so the review can give it to other
// nodes too (MachineConfigMultiPreview replays them on each node's own config). Only a patch
// that sets values does: a value is a put at its path, which is what the strategic merge does
// to it. A list (Talos merges some by key, appends others), a $patch directive, JSON patch
// operations or a document the draft does not have yet are {"fixable":false,"reason"}: such a
// patch is tried on one node with TalosPatchDraft. Local.
func TalosPatchEdits(draftYAML, patchYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	res, err := talosPatchEdits(draftYAML, []byte(patchYAML))
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

func talosPatchEdits(draftYAML string, patchYAML []byte) (snippetResult, error) {
	info := detectTalosPatch(patchYAML)
	if !info.Tryable {
		return snippetResult{Reason: "not a Talos patch that can be tried: " + info.Note}, nil
	}

	if info.Kind == talosPatchKindJSON {
		return snippetResult{Reason: "JSON patch operations" + talosOneNode}, nil
	}

	d, err := parseDraftDocs(draftYAML)
	if err != nil {
		return snippetResult{}, err
	}

	docs, err := yamlDocuments(patchYAML)
	if err != nil {
		return snippetResult{}, err
	}

	var edits []configEdit

	for _, raw := range docs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		doc, skip, reason := patchTarget(d, m)
		if reason != "" {
			return snippetResult{Reason: reason}, nil
		}

		more, reason, err := leafEdits(doc, nil, m, skip)
		if err != nil || reason != "" {
			return snippetResult{Reason: reason}, err
		}

		edits = append(edits, more...)
	}

	if len(edits) == 0 {
		return snippetResult{Reason: "the patch sets nothing"}, nil
	}

	return snippetResult{Fixable: true, Edits: edits}, nil
}

// patchTarget is the draft's document patch m changes and the keys of m that only name it.
func patchTarget(d configDocs, m map[string]any) (int, map[string]bool, string) {
	patch := patchDoc(m)
	if patch.Kind == talosLegacyDoc {
		doc, err := d.main()
		if err != nil {
			return 0, nil, err.Error()
		}

		return doc, map[string]bool{"version": true}, ""
	}

	doc := d.find(patch.Kind, patch.Name)
	if doc < 0 {
		what := patch.Kind
		if patch.Name != "" {
			what += " " + patch.Name
		}

		return 0, nil, "adds a " + what + " document" + talosOneNode
	}

	return doc, map[string]bool{"apiVersion": true, "kind": true, "name": true}, ""
}

// leafEdits are the puts of every value under m (at path in document doc), skip's keys left
// out at the top; reason says why the patch cannot be field edits.
func leafEdits(doc int, path []string, m map[string]any, skip map[string]bool) ([]configEdit, string, error) {
	var edits []configEdit

	for _, key := range keysSorted(m) {
		if skip[key] {
			continue
		}

		at := append(append([]string{}, path...), key)

		if strings.HasPrefix(key, "$patch") {
			return nil, "a $patch directive at " + strings.Join(path, ".") + talosOneNode, nil
		}

		switch v := m[key].(type) {
		case map[string]any:
			more, reason, err := leafEdits(doc, at, v, nil)
			if err != nil || reason != "" {
				return nil, reason, err
			}

			edits = append(edits, more...)
		case []any:
			return nil, "sets the list " + strings.Join(at, ".") + ", which Talos merges by its own rules" + talosOneNode, nil
		default:
			edit, err := put(doc, at, v)
			if err != nil {
				return nil, "", err
			}

			edits = append(edits, edit)
		}
	}

	return edits, "", nil
}
