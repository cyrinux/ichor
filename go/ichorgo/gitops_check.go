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

	// editCheckTimeout bounds the two builds of one directory.
	editCheckTimeout = 20 * time.Second
	// editCheckCandidates bounds the kustomizations up from the file tried in turn.
	editCheckCandidates = 4
	// editCheckMaxDepth bounds the walk through local bases looking for a remote one.
	editCheckMaxDepth = 16
	// editCheckMaxError caps an error message shown.
	editCheckMaxError = 600
)

var (
	errEditCheckPath = errors.New("the file's path is not in the repository")
	// editCheckLine is the line a YAML or kustomize error names.
	editCheckLine = regexp.MustCompile(`line (\d+)|\bL(\d+)\.C\d+`)
	// editCheckFile is a repository file a kustomize error names.
	editCheckFile = regexp.MustCompile(regexp.QuoteMeta(fluxSourceRoot) + `/([^\s:'"]+)`)
	// What a kustomize error echoes of a generator's literals and of an object.
	editCheckLiterals = regexp.MustCompile(`literal sources \[[^\]]*\]`)
	editCheckObject   = regexp.MustCompile(`in object \{.*\}`)
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
	// Broken: the build fails at the commit already, the same way: not the edit's doing.
	Broken bool   `json:"broken"`
	Note   string `json:"note,omitempty"`
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

	// The target is read with the edit in: a new kustomization is one already.
	kind, dirs := editCheckTargets(withEdit(files, p, content), p, spec)
	res.Kind, res.Dir = kind, dirs[0]

	yamlFile := strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")
	// A chart's templates are not YAML until Helm renders them.
	if kind != editCheckHelm && yamlFile && content != "" {
		if e, ok := yamlError(content, p); ok {
			res.Errors = append(res.Errors, e)

			return res
		}
	}

	switch kind {
	case editCheckHelm:
		res.OK = true
		res.Note = "rendered by Helm: not checked on the phone"
	case editCheckKustomize:
		checkKustomize(files, p, content, dirs, spec, &res)
	case editCheckPlain:
		if !yamlFile {
			res.Kind, res.OK = editCheckNone, true
			res.Note = "not a YAML file: nothing to build"

			return res
		}

		before, _ := yamlObjects(string(files[p]))
		after, _ := yamlObjects(content)
		res.OK = true
		diffRendered(markEncrypted(before), markEncrypted(after), &res)
	}

	return res
}

// editCheckTargets is what builds p: the Flux Kustomization's path whenever there is one
// (what the controller applies), else the kustomization directories up from p's, nearest
// first (at most editCheckCandidates), or the chart above it (helm); plain when there is none.
func editCheckTargets(files map[string][]byte, p string, spec *fluxKustomizeSpec) (string, []string) {
	if spec != nil {
		return editCheckKustomize, []string{strings.Trim(path.Clean("/"+spec.Path), "/.")}
	}

	var dirs []string

	for dir := path.Dir(p); ; dir = path.Dir(dir) {
		if dir == "." {
			dir = ""
		}

		if _, ok := files[path.Join(dir, "Chart.yaml")]; ok && len(dirs) == 0 {
			return editCheckHelm, []string{dir}
		}

		if hasKustomization(files, dir) && len(dirs) < editCheckCandidates {
			dirs = append(dirs, dir)
		}

		if dir == "" {
			break
		}
	}

	if len(dirs) == 0 {
		return editCheckPlain, []string{path.Dir(p)}
	}

	return editCheckKustomize, dirs
}

func hasKustomization(files map[string][]byte, dir string) bool {
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		if _, ok := files[path.Join(dir, name)]; ok {
			return true
		}
	}

	return false
}

// checkKustomize builds the candidate directories, nearest first, before and after the edit:
// the first whose render the edit changes (or breaks) is the one shown. A file that no
// candidate builds says so rather than "nothing changes".
func checkKustomize(files map[string][]byte, p, content string, dirs []string, spec *fluxKustomizeSpec, res *gitOpsEditCheck) {
	var first *gitOpsEditCheck

	for _, dir := range dirs {
		r := *res
		r.Dir = dir
		r.Errors = []editCheckError{}
		r.Diff.Resources = []kubeDiffResource{}
		checkKustomizeDir(files, p, content, spec, &r)

		if first == nil {
			first = &r
		}

		if !r.OK || len(r.Diff.Resources) > 0 {
			*res = r

			return
		}
	}

	*res = *first

	switch {
	case spec != nil && res.Dir != "" && !strings.HasPrefix(p, res.Dir+"/"):
		res.Note = "this file is outside the app's path " + res.Dir + ": the app's build is shown"
	case spec == nil && string(files[p]) != content:
		res.Note = "no rendered object changes: this file may be in none of the kustomizations above it"
	}
}

// checkKustomizeDir builds res.Dir before and after the edit and diffs the rendered objects.
func checkKustomizeDir(files map[string][]byte, p, content string, spec *fluxKustomizeSpec, res *gitOpsEditCheck) {
	if ref := remoteBase(withEdit(files, p, content), res.Dir, 0); ref != "" {
		res.Errors = append(res.Errors, editCheckError{Message: "remote base " + ref + " is not fetched on the phone: the build was not checked"})

		return
	}

	out, err := buildBounded(func() buildBoth { return buildBeforeAfter(files, p, content, res.Dir, spec) })
	if err != nil {
		res.Errors = append(res.Errors, editCheckError{Message: err.Error()})

		return
	}

	switch {
	case out.afterErr != nil && out.beforeErr != nil && cleanBuildError(out.afterErr.Error()) == cleanBuildError(out.beforeErr.Error()):
		// Not the edit's doing: the commit does not build either.
		res.Broken = true
		res.Errors = append(res.Errors, kustomizeError(out.afterErr))
		res.Note = "the kustomization already fails at this commit, before this edit"
	case out.afterErr != nil:
		res.Errors = append(res.Errors, kustomizeError(out.afterErr))
	default:
		before := out.before
		if out.beforeErr != nil {
			before = nil
			res.Note = "the kustomization did not build at this commit: this edit makes it build"
		}

		res.OK = true
		diffRendered(before, out.after, res)
	}
}

// withEdit is files with content at p, or without p when content is "" (the contents shared).
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

// buildBoth is a directory built at the commit and with the edit.
type buildBoth struct {
	before, after       []map[string]any
	beforeErr, afterErr error
}

// editCheckBusy lets one build run at a time: krusty cannot be stopped, so a build past its
// time keeps its tree until it ends, and a new check waits for it rather than adding one.
var editCheckBusy = make(chan struct{}, 1)

// buildBounded runs build within editCheckTimeout, one at a time.
func buildBounded(build func() buildBoth) (buildBoth, error) {
	select {
	case editCheckBusy <- struct{}{}:
	default:
		return buildBoth{}, errors.New("a previous build check is still running: retry in a moment")
	}

	done := make(chan buildBoth, 1)

	go func() {
		// Free the slot before handing the result back: a caller that has its result can
		// start the next check at once and must not find this one still holding it.
		var b buildBoth

		defer func() {
			if r := recover(); r != nil {
				// The edited build's: the commit's is not known to fail the same way.
				b = buildBoth{afterErr: fmt.Errorf("the build failed: %v", r)}
			}

			<-editCheckBusy
			done <- b
		}()

		b = build()
	}()

	select {
	case b := <-done:
		return b, nil
	case <-time.After(editCheckTimeout):
		return buildBoth{}, fmt.Errorf("the build took over %s", editCheckTimeout)
	}
}

// buildBeforeAfter lays files out once under fluxSourceRoot, builds dir, then puts the edit
// in (and the kustomization the Flux build rewrote back) and builds it again.
func buildBeforeAfter(files map[string][]byte, p, content, dir string, spec *fluxKustomizeSpec) buildBoth {
	fsys := filesys.MakeFsInMemory()
	for f, b := range files {
		if err := fsys.WriteFile(path.Join(fluxSourceRoot, f), b); err != nil {
			return buildBoth{beforeErr: err, afterErr: err}
		}
	}

	var out buildBoth

	out.before, out.beforeErr = buildRepoDir(fsys, dir, spec)

	for _, name := range konfig.RecognizedKustomizationFileNames() {
		k := path.Join(fluxSourceRoot, dir, name)
		if b, ok := files[path.Join(dir, name)]; ok {
			_ = fsys.WriteFile(k, b)
		} else if fsys.Exists(k) {
			_ = fsys.RemoveAll(k)
		}
	}

	target := path.Join(fluxSourceRoot, p)
	if content == "" {
		_ = fsys.RemoveAll(target)
	} else if err := fsys.WriteFile(target, []byte(content)); err != nil {
		out.afterErr = err

		return out
	}

	out.after, out.afterErr = buildRepoDir(fsys, dir, spec)

	return out
}

// buildRepoDir builds dir of the tree under fluxSourceRoot: with spec as kustomize-controller
// does, else as `kustomize build dir`.
func buildRepoDir(fsys filesys.FileSystem, dir string, spec *fluxKustomizeSpec) ([]map[string]any, error) {
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

		objs = append(objs, obj)
	}

	return markEncrypted(objs), nil
}

// markEncrypted reduces SOPS objects to their identity, marked: never compared, never shown.
func markEncrypted(objs []map[string]any) []map[string]any {
	for _, obj := range objs {
		if _, ok := obj["sops"]; ok {
			for _, k := range []string{"sops", "data", "stringData", "spec"} {
				delete(obj, k)
			}

			obj[editCheckEncrypted] = true
		}
	}

	return objs
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
	e := editCheckError{Message: cleanBuildError(msg)}

	if m := editCheckFile.FindStringSubmatch(msg); m != nil {
		e.Path = m[1]
	}

	e.Line = errorLine(msg)

	return e
}

// cleanBuildError is a kustomize error without what it echoes of the files: a patch or a
// generator's literals can hold a Secret's values, and the error is shown on screen.
func cleanBuildError(msg string) string {
	msg = strings.ReplaceAll(msg, fluxSourceRoot+"/", "")
	// The config and what follows it echo the patch: nothing of it is kept.
	if i := strings.Index(msg, " with config:"); i >= 0 {
		msg = msg[:i] + " (the patch is not shown: check its YAML)"
	}

	msg = editCheckLiterals.ReplaceAllString(msg, "literal sources […]")
	msg = editCheckObject.ReplaceAllString(msg, "in object {…}")

	if len(msg) > editCheckMaxError {
		msg = msg[:editCheckMaxError] + "…"
	}

	return msg
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
