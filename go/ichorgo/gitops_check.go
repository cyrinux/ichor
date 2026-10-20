package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
	"sigs.k8s.io/kustomize/api/konfig"
	"sigs.k8s.io/kustomize/api/krusty"
	kustypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	sigsyaml "sigs.k8s.io/yaml"
)

// The editor's Check, `kustomize build` before `git push`: the edited file laid over the
// repository snapshot at the commit, the kustomization it belongs to built before and after
// (with krusty, as the Flux build does), and the rendered objects that change, or why the
// build fails. Nothing reaches the cluster.

const (
	editCheckKustomize = "kustomize"
	editCheckPlain     = "plain"
	editCheckHelm      = "helm"
	editCheckNone      = "none"

	editCheckTimeout = 10 * time.Second
	// editCheckMaxDepth bounds the walk through local bases looking for a remote one.
	editCheckMaxDepth = 16
)

var (
	errEditCheckPath = errors.New("the file's path is not in the repository")
	// editCheckLine is the line a YAML or kustomize error names.
	editCheckLine = regexp.MustCompile(`line (\d+)|\bL(\d+)\.C\d+`)
	// editCheckFile is a repository file a kustomize error names.
	editCheckFile = regexp.MustCompile(regexp.QuoteMeta(fluxSourceRoot) + `/([^\s:'"]+)`)
)

type editCheckError struct {
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
}

type gitOpsEditCheck struct {
	SHA    string           `json:"sha"`
	Kind   string           `json:"kind"`
	Dir    string           `json:"dir"`
	OK     bool             `json:"ok"`
	Errors []editCheckError `json:"errors"`
	// Diff lists the rendered objects added, changed or removed (unchanged ones are counted).
	Diff      gitOpsDiff `json:"diff"`
	Unchanged int        `json:"unchanged"`
	Note      string     `json:"note,omitempty"`
}

// GitOpsEditCheck checks an edit of the file at path (from the repository root) of repoURL at
// ref before it is proposed: content laid over the repository's snapshot (removed when ""),
// then {"sha","kind":"kustomize|plain|helm|none","dir","ok","errors":[{"message","path","line"}],
// "diff":<GitOpsDiff of the rendered objects that change>,"unchanged","note"}. kustomize: the
// nearest kustomization from the file's directory up, built before and after (fluxJSON, a
// Flux Kustomization's spec, builds its path with its settings and inline postBuild
// variables instead); plain: the file's own documents; helm: a chart, which is not rendered
// on the phone. Secret values are masked; SOPS objects keep their identity only; a remote
// base is reported, not fetched. Local apart from the snapshot download.
func GitOpsEditCheck(hostJSON, repoURL, ref, filePath, content, fluxJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	p := strings.TrimPrefix(path.Clean("/"+filePath), "/")
	if p == "" || p == "." {
		return "", errEditCheckPath
	}

	var spec *fluxKustomizeSpec
	if strings.TrimSpace(fluxJSON) != "" {
		spec = &fluxKustomizeSpec{}
		if err := json.Unmarshal([]byte(fluxJSON), spec); err != nil {
			return "", fmt.Errorf("invalid Flux spec: %w", err)
		}
	}

	snap, _, err := loadRepoSnapshot(hostJSON, repoURL, ref)
	if err != nil {
		return "", err
	}

	res := checkEdit(snap.files, p, content, spec)
	res.SHA = snap.sha

	return toJSON(res)
}

// checkEdit is GitOpsEditCheck over a snapshot's files.
func checkEdit(files map[string][]byte, p, content string, spec *fluxKustomizeSpec) gitOpsEditCheck {
	res := gitOpsEditCheck{Errors: []editCheckError{}, Diff: gitOpsDiff{Name: p, Resources: []kubeDiffResource{}, Warnings: []string{}}}

	yamlFile := strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")
	if yamlFile && content != "" {
		if e, ok := yamlError(content, p); ok {
			res.Kind, res.Dir = editCheckNone, path.Dir(p)
			res.Errors = append(res.Errors, e)

			return res
		}
	}

	res.Kind, res.Dir = editCheckTarget(files, p, spec)

	switch res.Kind {
	case editCheckHelm:
		res.OK = true
		res.Note = "rendered by Helm: not checked on the phone"
	case editCheckKustomize:
		checkKustomize(files, p, content, spec, &res)
	case editCheckPlain:
		if !yamlFile {
			res.Kind, res.OK = editCheckNone, true
			res.Note = "not a YAML file: nothing to build"

			return res
		}

		before, _ := yamlObjects(string(files[p]))
		after, _ := yamlObjects(content)
		res.OK = true
		diffRendered(before, after, &res)
	}

	return res
}

// editCheckTarget is what builds p: the Flux Kustomization's path when p is under it, else
// the nearest directory up from p's with a Chart.yaml (helm) or a kustomization (kustomize);
// plain when there is none.
func editCheckTarget(files map[string][]byte, p string, spec *fluxKustomizeSpec) (string, string) {
	if spec != nil {
		dir := strings.Trim(path.Clean("/"+spec.Path), "/")
		if dir == "" || dir == "." || strings.HasPrefix(p, dir+"/") {
			return editCheckKustomize, dir
		}
	}

	for dir := path.Dir(p); ; dir = path.Dir(dir) {
		if dir == "." {
			dir = ""
		}

		if _, ok := files[path.Join(dir, "Chart.yaml")]; ok {
			return editCheckHelm, dir
		}

		for _, name := range konfig.RecognizedKustomizationFileNames() {
			if _, ok := files[path.Join(dir, name)]; ok {
				return editCheckKustomize, dir
			}
		}

		if dir == "" {
			return editCheckPlain, path.Dir(p)
		}
	}
}

// checkKustomize builds res.Dir before and after the edit and diffs the rendered objects.
func checkKustomize(files map[string][]byte, p, content string, spec *fluxKustomizeSpec, res *gitOpsEditCheck) {
	after := withEdit(files, p, content)

	if ref := remoteBase(after, res.Dir, 0); ref != "" {
		res.Errors = append(res.Errors, editCheckError{Message: "remote base " + ref + " is not fetched on the phone: the build was not checked"})

		return
	}

	built, err := buildWithin(after, res.Dir, spec)
	if err != nil {
		res.Errors = append(res.Errors, kustomizeError(err))

		return
	}

	// The commit's own build failing is not the edit's doing: everything then shows as added.
	before, err := buildWithin(files, res.Dir, spec)
	if err != nil {
		before = nil
		res.Note = "the kustomization does not build at this commit either: " + strings.ReplaceAll(err.Error(), fluxSourceRoot+"/", "")
	}

	res.OK = true
	diffRendered(before, built, res)
}

// withEdit is files with content at p, or without p when content is "".
func withEdit(files map[string][]byte, p, content string) map[string][]byte {
	out := make(map[string][]byte, len(files)+1)
	for k, v := range files {
		out[k] = v
	}

	if content == "" {
		delete(out, p)
	} else {
		out[p] = []byte(content)
	}

	return out
}

// buildWithin builds dir of files in a fresh in-memory tree, within editCheckTimeout.
func buildWithin(files map[string][]byte, dir string, spec *fluxKustomizeSpec) ([]map[string]any, error) {
	type result struct {
		objs []map[string]any
		err  error
	}

	done := make(chan result, 1)

	go func() {
		objs, err := buildRepoDir(files, dir, spec)
		done <- result{objs, err}
	}()

	select {
	case r := <-done:
		return r.objs, r.err
	case <-time.After(editCheckTimeout):
		return nil, fmt.Errorf("the build took over %s", editCheckTimeout)
	}
}

// buildRepoDir lays files out under fluxSourceRoot and builds dir: with spec as
// kustomize-controller does, else as `kustomize build dir`.
func buildRepoDir(files map[string][]byte, dir string, spec *fluxKustomizeSpec) ([]map[string]any, error) {
	fsys := filesys.MakeFsInMemory()
	for p, b := range files {
		if err := fsys.WriteFile(path.Join(fluxSourceRoot, p), b); err != nil {
			return nil, err
		}
	}

	if spec != nil {
		s := *spec
		s.Path = dir

		var vars map[string]string
		if s.PostBuild != nil {
			vars = s.PostBuild.Substitute
		}

		built, err := buildFluxKustomization(fsys, s, "", "", vars)
		if err != nil {
			return nil, err
		}

		objs := make([]map[string]any, 0, len(built))
		for _, b := range built {
			if b.encrypted {
				b.obj[editCheckEncrypted] = true
			}

			// The owner labels name no Kustomization here: they would only add noise.
			if labels, ok := yamlPath(b.obj, "metadata", "labels").(map[string]any); ok {
				delete(labels, fluxOwnerName)
				delete(labels, fluxOwnerNamespace)
			}

			objs = append(objs, b.obj)
		}

		return objs, nil
	}

	opts := krusty.MakeDefaultOptions()
	// The tree holds the repository only: a ../base within it is allowed, nothing else is there.
	opts.LoadRestrictions = kustypes.LoadRestrictionsNone

	resmap, err := krusty.MakeKustomizer(opts).Run(fsys, path.Join(fluxSourceRoot, dir))
	if err != nil {
		return nil, err
	}

	objs := make([]map[string]any, 0, resmap.Size())
	for _, r := range resmap.Resources() {
		y, err := r.AsYAML()
		if err != nil {
			return nil, err
		}

		var obj map[string]any
		if err := yaml.Unmarshal(y, &obj); err != nil {
			return nil, err
		}

		if _, ok := obj["sops"]; ok {
			// Encrypted: its identity only, as the Flux build keeps it.
			for _, k := range []string{"sops", "data", "stringData", "spec"} {
				delete(obj, k)
			}

			obj[editCheckEncrypted] = true
		}

		objs = append(objs, obj)
	}

	return objs, nil
}

// editCheckEncrypted marks a SOPS object in a build: never compared, never shown.
const editCheckEncrypted = "\x00encrypted"

// remoteBase is the first remote resource, base or component the kustomization of dir pulls
// in, through its local directories; "" when all are local.
func remoteBase(files map[string][]byte, dir string, depth int) string {
	if depth > editCheckMaxDepth {
		return ""
	}

	var kus kustypes.Kustomization

	for _, name := range konfig.RecognizedKustomizationFileNames() {
		if b, ok := files[path.Join(dir, name)]; ok {
			if err := sigsyaml.Unmarshal(b, &kus); err != nil {
				return ""
			}

			break
		}
	}

	//lint:ignore SA1019 bases is deprecated in kustomize, still read by it
	for _, ref := range slices.Concat(kus.Resources, kus.Bases, kus.Components) {
		if !isLocalRelativePath(ref) {
			return ref
		}

		sub := path.Clean(path.Join(dir, ref))
		if hasDir(files, sub) {
			if r := remoteBase(files, sub, depth+1); r != "" {
				return r
			}
		}
	}

	return ""
}

// hasDir tells files holds something under dir.
func hasDir(files map[string][]byte, dir string) bool {
	prefix := strings.TrimPrefix(dir+"/", "./")
	for p := range files {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}

	return false
}

// kustomizeError is a build error with the repository file and line it names, when it does.
func kustomizeError(err error) editCheckError {
	msg := err.Error()
	e := editCheckError{Message: strings.ReplaceAll(msg, fluxSourceRoot+"/", "")}

	if m := editCheckFile.FindStringSubmatch(msg); m != nil {
		e.Path = m[1]
	}

	e.Line = errorLine(msg)

	return e
}

// yamlError is the first YAML error of content, with its line.
func yamlError(content, p string) (editCheckError, bool) {
	if _, err := yamlObjects(content); err != nil {
		return editCheckError{Message: err.Error(), Path: p, Line: errorLine(err.Error())}, true
	}

	return editCheckError{}, false
}

// errorLine is the line an error names ("line 3", go-yaml's "L3.C9"), 0 when none.
func errorLine(msg string) int {
	m := editCheckLine.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}

	n, _ := strconv.Atoi(m[1] + m[2])

	return n
}

// yamlObjects are the objects (mappings) of every document of content.
func yamlObjects(content string) ([]map[string]any, error) {
	docs, err := yamlDocuments([]byte(content))
	if err != nil {
		return nil, err
	}

	var objs []map[string]any

	for _, d := range docs {
		if m, ok := d.(map[string]any); ok {
			objs = append(objs, m)
		}
	}

	return objs, nil
}

// diffRendered lists in res the objects of after that are added, changed or removed against
// before, by identity, Secret values masked.
func diffRendered(before, after []map[string]any, res *gitOpsEditCheck) {
	masker := newKubeDiffMasker()
	was := map[string]map[string]any{}

	for _, o := range before {
		was[objectKey(o)] = o
	}

	seen := map[string]bool{}

	for _, o := range after {
		key := objectKey(o)
		seen[key] = true
		res.addObject(was[key], o, masker)
	}

	for _, o := range before {
		if !seen[objectKey(o)] {
			res.addObject(o, nil, masker)
		}
	}

	sortDiffResources(res.Diff.Resources)
}

// addObject compares one object's renders: unchanged ones are only counted.
func (res *gitOpsEditCheck) addObject(before, after map[string]any, masker *kubeDiffMasker) {
	obj := after
	if obj == nil {
		obj = before
	}
	group, version := splitAPIVersion(yamlString(obj, "apiVersion"))
	r := kubeDiffResource{
		Group: group, Version: version, Kind: yamlString(obj, "kind"),
		Namespace: yamlString(obj, "metadata", "namespace"), Name: yamlString(obj, "metadata", "name"),
	}

	switch {
	case before == nil:
		r.Change = diffChangeCreated
	case after == nil:
		r.Change = diffChangeDeleted
	}

	// Encrypted (SOPS): never compared nor shown; only its coming or going is.
	if obj[editCheckEncrypted] == true {
		if r.Change == "" {
			res.Unchanged++

			return
		}

		res.Diff.Resources = append(res.Diff.Resources, r)

		return
	}

	from, to := diffNormalize(before, masker), diffNormalize(after, masker)
	if from == to {
		res.Unchanged++

		return
	}

	if r.Change == "" {
		r.Change = diffChangeChanged
	}

	r.Diff = unifiedDiff(from, to, "a/"+r.Kind+"/"+r.Name, "b/"+r.Kind+"/"+r.Name)
	if len(r.Diff) > diffMaxBytes {
		cut := strings.LastIndexByte(r.Diff[:diffMaxBytes], '\n')
		r.Diff, r.Truncated = r.Diff[:cut+1], true
	}

	res.Diff.Resources = append(res.Diff.Resources, r)
}

// objectKey is an object's identity: group, kind, namespace and name.
func objectKey(o map[string]any) string {
	group, _ := splitAPIVersion(yamlString(o, "apiVersion"))

	return group + "/" + yamlString(o, "kind") + "/" + yamlString(o, "metadata", "namespace") + "/" + yamlString(o, "metadata", "name")
}

func yamlString(m map[string]any, keys ...string) string {
	s, _ := yamlPath(m, keys...).(string)

	return s
}
