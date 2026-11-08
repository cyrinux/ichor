package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pathpkg "path"
	"slices"
	"strconv"
	"strings"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
)

// Talos from Git: the files of the cluster repository that configure a node (its own patch,
// the shared ones, talhelper's talconfig.yaml, the secrets files), and what differs between
// the repository and the node, found by applying those patches onto the node's live config
// with Talos's own configpatcher, as `talosctl gen config --config-patch` and
// `talhelper genconfig` fold them into the configs.

const (
	talosFileNode      = "node"
	talosFileShared    = "shared"
	talosFileTalconfig = "talconfig"
	talosFileSecrets   = "secrets"

	talosGenTalhelper = "talhelper"
	talosGenTalosctl  = "talosctl"
	talosGenUnknown   = "unknown"

	// talosInlineMark joins talconfig.yaml's path and the line of one of its inline patches.
	talosInlineMark = "#L"
	talosDiffName   = "machine config"
)

var (
	errTalosRepoNoFiles = errors.New("pick at least one file to compare")
	errTalosRepoPrivacy = errors.New("turn privacy mode off to compare the node with Git: masked names and addresses must not be sent to a node")
)

// talosSecretPaths are the config keys a repository patch may not set: the node keeps its own.
var talosSecretPaths = [][]string{
	{"machine", "token"}, {"machine", "ca"}, {"machine", "acceptedCAs"},
	{"cluster", "id"}, {"cluster", "secret"}, {"cluster", "token"}, {"cluster", "ca"},
	{"cluster", "aggregatorCA"}, {"cluster", "serviceAccount"}, {"cluster", "acceptedCAs"},
	{"cluster", "secretboxEncryptionSecret"}, {"cluster", "aescbcEncryptionSecret"},
	{"cluster", "etcd", "ca"},
}

type talosRepoFile struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // node, shared, talconfig, secrets
	Why  string `json:"why"`
	Line int    `json:"line,omitempty"`
	// Patch: it can be applied (secrets files and talconfig.yaml itself cannot).
	Patch bool `json:"patch"`
}

type talosRepoFiles struct {
	SHA       string          `json:"sha"`
	Generator string          `json:"generator"` // talhelper, talosctl, unknown
	Files     []talosRepoFile `json:"files"`
	Note      string          `json:"note"`
}

type talosRepoSkip struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

type talosRepoCompare struct {
	SHA         string          `json:"sha"`
	Files       []string        `json:"files"` // applied, in order
	Diff        gitOpsDiff      `json:"diff"`
	Same        bool            `json:"same"`
	NeedsReboot bool            `json:"needsReboot"`
	Skipped     []talosRepoSkip `json:"skipped"`
	// Draft is the node's config with the patches applied, redacted: the editor's draft for
	// "Edit from this diff".
	Draft string `json:"draft"`
}

// TalosRepoFiles lists the files of repoURL at ref (the snapshot of GitRepoSnapshotPrepare)
// that configure the node hostname of role (controlplane or worker), under talosDir (the whole
// repository when blank): {"sha","generator","files":[{"path","kind","why","line","patch"}],
// "note"}. A node file sets the node's hostname (machine.network.hostname, a HostnameConfig
// document) or is named after it; a shared file is any other Talos patch that is not another
// node's; with a talconfig.yaml that lists the node, its patches in talhelper's order instead;
// secrets files (SOPS, keys) are listed but never applied. kind says which, why the rule. A
// repository too large for a snapshot has talosDir alone read through the forge (refused
// when talosDir is blank), which note says.
func TalosRepoFiles(hostJSON, repoURL, ref, talosDir, hostname, role string) (out string, err error) {
	defer maskResult(&out, &err)

	hostname = privacy.unmaskNode(hostname)

	repo, err := readTalosRepo(hostJSON, repoURL, ref, talosDirReader(talosDir))
	if err != nil {
		return "", err
	}

	found := findTalosRepoFiles(repo.files, talosDir, hostname, role)
	found.SHA = repo.sha
	found.Note = strings.TrimSpace(repo.note + " " + found.Note)

	return toJSON(found)
}

// TalosRepoCompare applies the files patchPathsJSON names (a JSON array of TalosRepoFiles
// paths, in that order: an inline talconfig patch is "talconfig.yaml#L<line>") onto node's
// live config and tells what differs from Git: {"sha","files","diff":<TextDiff>,"same",
// "needsReboot","skipped":[{"path","why"}],"draft"}. The config is read with its secrets
// (os:admin) and redacted before anything leaves Go; a patch that does not load, still holds
// a ${...} placeholder or sets a secret is skipped. The node checks the patched config with an
// ApplyConfiguration dry run, which also says whether applying it needs a reboot. On a
// repository too large for a snapshot, only the chosen files are read from the forge. Refused
// in privacy mode.
func TalosRepoCompare(configYAML, contextName, hostJSON, repoURL, ref, node, hostname, patchPathsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if privacy.isEnabled() {
		return "", errTalosRepoPrivacy
	}

	var paths []string
	if err := json.Unmarshal([]byte(patchPathsJSON), &paths); err != nil || len(paths) == 0 {
		return "", errTalosRepoNoFiles
	}

	snap, err := readTalosRepo(hostJSON, repoURL, ref, talosPathsReader(paths))
	if err != nil {
		return "", err
	}

	patches, used, skipped := loadTalosPatches(snap.files, paths)
	if len(patches) == 0 {
		return "", fmt.Errorf("none of the chosen files applies: %s %s", skipped[0].Path, skipped[0].Why)
	}

	compare := func(ctx context.Context, a configApplier) (string, error) {
		res, err := compareTalosRepo(ctx, a, patches)
		if err != nil {
			return "", err
		}

		res.SHA, res.Files, res.Skipped = snap.sha, used, skipped

		return toJSON(res)
	}

	if isTalosDemoContext(configYAML, contextName) {
		text, err := demoRead("NodeMachineConfig", configYAML, contextName, node)
		if err != nil {
			return "", err
		}

		if canonical, err := canonicalConfig(text); err == nil {
			text = canonical
		}

		return compare(context.Background(), demoConfigApplier{text})
	}

	return withNodeSession(configYAML, contextName, node, configPreviewTimeout, func(ctx context.Context, s *session) (string, error) {
		return compare(ctx, nodeConfigApplier{s, node})
	})
}

// compareTalosRepo applies patches onto a's config and diffs the redacted configs.
func compareTalosRepo(ctx context.Context, a configApplier, patches []configpatcher.Patch) (talosRepoCompare, error) {
	snap, err := a.current(ctx)
	if err != nil {
		return talosRepoCompare{}, err
	}

	patched, err := configpatcher.Apply(configpatcher.WithBytes([]byte(snap.plain)), patches)
	if err != nil {
		return talosRepoCompare{}, fmt.Errorf("apply the repository's patches: %w", err)
	}

	provider, err := patched.Config()
	if err != nil {
		return talosRepoCompare{}, fmt.Errorf("the patched config is not a valid machine config: %w", err)
	}

	after, err := provider.RedactSecrets(redacted).EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return talosRepoCompare{}, fmt.Errorf("encode machine config: %w", err)
	}

	res := talosRepoCompare{
		Diff:  textDiff(snap.redacted, after, talosDiffName),
		Same:  strings.TrimSpace(snap.redacted) == strings.TrimSpace(after),
		Draft: after,
	}
	if res.Same {
		return res, nil
	}

	data, err := provider.Bytes()
	if err != nil {
		return talosRepoCompare{}, fmt.Errorf("encode machine config: %w", err)
	}

	result, err := a.apply(ctx, data, machineapi.ApplyConfigurationRequest_AUTO, true, 0)
	if err != nil {
		return talosRepoCompare{}, err
	}

	res.NeedsReboot = needsReboot(result)

	return res, nil
}

// loadTalosPatches loads each path's patch from files: the ones applied (with their paths, in
// order) and the ones skipped, with why.
func loadTalosPatches(files map[string][]byte, paths []string) ([]configpatcher.Patch, []string, []talosRepoSkip) {
	var (
		patches []configpatcher.Patch
		used    = []string{}
		skipped = []talosRepoSkip{}
	)

	for _, p := range paths {
		content, err := talosPatchContent(files, p)
		if err == nil {
			err = checkTalosPatch(content)
		}

		var patch configpatcher.Patch
		if err == nil {
			patch, err = configpatcher.LoadPatch(content)
		}

		if err != nil {
			skipped = append(skipped, talosRepoSkip{Path: p, Why: err.Error()})

			continue
		}

		patches = append(patches, patch)
		used = append(used, p)
	}

	return patches, used, skipped
}

// talosPatchContent is the text of the patch at p: a file, or an inline talconfig patch.
func talosPatchContent(files map[string][]byte, p string) ([]byte, error) {
	file, mark, inline := strings.Cut(p, talosInlineMark)

	content, ok := files[file]
	if !ok {
		return nil, errors.New("is not in the repository at this commit")
	}

	if !inline {
		return content, nil
	}

	line, err := strconv.Atoi(mark)
	if err != nil {
		return nil, errors.New("names no line of talconfig.yaml")
	}

	return talconfigInlinePatch(content, line)
}

// checkTalosPatch refuses a patch that still holds a talhelper placeholder or sets a secret.
func checkTalosPatch(content []byte) error {
	if strings.Contains(string(content), "${") {
		return errors.New("holds a ${...} substitution the app does not resolve (talenv)")
	}

	docs, err := yamlDocuments(content)
	if err != nil {
		return fmt.Errorf("is not YAML: %w", err)
	}

	if key, ok := setsTalosSecret(docs); ok {
		return fmt.Errorf("sets %s: the node keeps its own secrets", key)
	}

	return nil
}

// findTalosRepoFiles is TalosRepoFiles over a snapshot's files.
func findTalosRepoFiles(files map[string][]byte, talosDir, hostname, role string) talosRepoFiles {
	dir := strings.Trim(talosDir, "/")
	paths := talosYAMLPaths(files, dir)

	for _, p := range paths {
		if base := pathpkg.Base(p); base == "talconfig.yaml" || base == "talconfig.yml" {
			if found, ok := talhelperFiles(files, paths, p, hostname, role); ok {
				return found
			}
		}
	}

	return plainTalosFiles(files, paths, dir, hostname)
}

// talosYAMLPaths are the YAML files under dir, sorted.
func talosYAMLPaths(files map[string][]byte, dir string) []string {
	var paths []string

	for p := range files {
		if dir != "" && !strings.HasPrefix(p, dir+"/") {
			continue
		}

		if ext := pathpkg.Ext(p); ext == ".yaml" || ext == ".yml" {
			paths = append(paths, p)
		}
	}

	slices.Sort(paths)

	return paths
}

// plainTalosFiles sorts the patches of a repository without talconfig.yaml: the shared ones
// first, then the node's, as `talosctl gen config --config-patch` takes them.
func plainTalosFiles(files map[string][]byte, paths []string, dir, hostname string) talosRepoFiles {
	var shared, own, secrets []talosRepoFile

	where := dir
	if where == "" {
		where = "the repository"
	}

	for _, p := range paths {
		docs, err := yamlDocuments(files[p])
		if err != nil {
			continue
		}

		if why, ok := secretsFile(p, docs); ok {
			secrets = append(secrets, talosRepoFile{Path: p, Kind: talosFileSecrets, Why: why})

			continue
		}

		if !isTalosPatch(docs) {
			continue
		}

		if why, ok := nodeFile(p, docs, hostname); ok {
			own = append(own, talosRepoFile{Path: p, Kind: talosFileNode, Why: why, Patch: true})

			continue
		}

		if len(talosHostnames(docs)) > 0 || slices.Contains(strings.Split(pathpkg.Dir(p), "/"), "nodes") {
			continue // another node's
		}

		shared = append(shared, talosRepoFile{Path: p, Kind: talosFileShared, Why: "a Talos patch under " + where, Patch: true})
	}

	found := talosRepoFiles{Generator: talosGenUnknown, Files: slices.Concat(shared, own, secrets)}
	if len(shared)+len(own) > 0 {
		found.Generator = talosGenTalosctl
	}

	switch {
	case len(own) == 0 && len(shared) > 0:
		found.Note = "No file sets this node's hostname: only the shared patches apply."
	case len(own)+len(shared) == 0:
		found.Note = "No Talos patch found under " + where + "."
	}

	return found
}

// nodeFile tells a file of the node hostname: it sets that hostname, or is named after it.
func nodeFile(p string, docs []any, hostname string) (string, bool) {
	for _, h := range talosHostnames(docs) {
		if same, short := sameHost(h, hostname); same {
			if short {
				return "sets hostname " + h + " (the node's short name)", true
			}

			return "sets hostname " + h, true
		}
	}

	if len(talosHostnames(docs)) > 0 {
		return "", false
	}

	base := pathpkg.Base(p)
	for _, name := range []string{hostname, shortHost(hostname)} {
		if name == "" {
			continue
		}

		for _, ext := range []string{".yaml", ".yml", ".patch.yaml", ".patch.yml"} {
			if base == name+ext {
				return "named after the node", true
			}
		}

		if pathpkg.Base(pathpkg.Dir(p)) == name {
			return "in the node's directory", true
		}
	}

	return "", false
}

// sameHost tells hostnames a and b apart, the short one matching an FQDN's first label.
func sameHost(a, b string) (same, short bool) {
	if a == "" || b == "" {
		return false, false
	}

	if strings.EqualFold(a, b) {
		return true, false
	}

	label := shortHost(a)

	return label != "" && label == shortHost(b), true
}

// talosHostnames are the hostnames docs set: machine.network.hostname, a HostnameConfig's.
func talosHostnames(docs []any) []string {
	var hosts []string

	for _, d := range docs {
		m, _ := d.(map[string]any)
		if h, ok := yamlPath(m, "machine", "network", "hostname").(string); ok && h != "" {
			hosts = append(hosts, h)
		}

		if kind, _ := m["kind"].(string); kind == "HostnameConfig" {
			if h, ok := m["hostname"].(string); ok && h != "" {
				hosts = append(hosts, h)
			}
		}
	}

	return hosts
}

// isTalosPatch tells docs make a Talos patch: a machine or cluster section, a multi-doc
// config document, or JSON patch operations on them.
func isTalosPatch(docs []any) bool {
	for _, d := range docs {
		switch v := d.(type) {
		case map[string]any:
			if v["machine"] != nil || v["cluster"] != nil {
				return true
			}

			if api, _ := v["apiVersion"].(string); api == "v1alpha1" && v["kind"] != nil {
				return true
			}
		case []any:
			for _, op := range v {
				m, _ := op.(map[string]any)
				if p, _ := m["path"].(string); m["op"] != nil && (strings.HasPrefix(p, "/machine") || strings.HasPrefix(p, "/cluster")) {
					return true
				}
			}
		}
	}

	return false
}

// secretsFile tells a file of secrets: SOPS-encrypted, or setting the cluster's keys.
func secretsFile(p string, docs []any) (string, bool) {
	if strings.HasSuffix(p, ".sops.yaml") || strings.HasSuffix(p, ".sops.yml") {
		return "encrypted with SOPS", true
	}

	for _, d := range docs {
		if m, _ := d.(map[string]any); m["sops"] != nil {
			return "encrypted with SOPS", true
		}
	}

	if key, ok := setsTalosSecret(docs); ok {
		return "sets " + key, true
	}

	return "", false
}

// setsTalosSecret is the first secret key docs set.
func setsTalosSecret(docs []any) (string, bool) {
	for _, d := range docs {
		m, _ := d.(map[string]any)
		for _, keys := range talosSecretPaths {
			if yamlPath(m, keys...) != nil {
				return strings.Join(keys, "."), true
			}
		}
	}

	return "", false
}

// yamlPath is the value at keys in m, nil when any is missing.
func yamlPath(m map[string]any, keys ...string) any {
	var v any = m

	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}

		v = mm[k]
	}

	return v
}
