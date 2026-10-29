package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

// UpdateEditTarget: from one item of ClusterUpdates (an image or a chart, its owner and its
// edit) to the file in Git that sets its version, the document and the line, and the quick
// edit (gitops_quickedit.go) that bumps it. A workload's image is looked up in its owner's
// sources (GitOpsSourceFind); a chart version is in the HelmRelease or Application itself,
// looked up in the sources of the app that applied it. Nothing is written.

// updateEditItem is the part of an images[] or charts[] item UpdateEditTarget reads; Version
// is the candidate picked (a chip), "" for the edit's own (the smallest step).
type updateEditItem struct {
	Repo      string              `json:"repo"`
	Name      string              `json:"name"`
	Workloads []updateWorkloadRef `json:"workloads"`
	Owner     *updateOwner        `json:"owner"`
	Edit      *updateEdit         `json:"edit"`
	Version   string              `json:"version"`
}

// updateEditApp is the Argo CD or Flux app whose source holds the file.
type updateEditApp struct {
	Tool      string `json:"tool"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// updateQuickEdit is GitOpsQuickEdit's editJSON for the file found.
type updateQuickEdit struct {
	Op             string `json:"op"`
	DocumentIndex  int    `json:"documentIndex"`
	Kind           string `json:"kind,omitempty"`
	Name           string `json:"name,omitempty"`
	Container      string `json:"container,omitempty"`
	KustomizeImage string `json:"kustomizeImage,omitempty"`
	Image          string `json:"image,omitempty"`
	Path           string `json:"path,omitempty"`
	Value          string `json:"value,omitempty"`
}

type updateEditTarget struct {
	App           *updateEditApp   `json:"app,omitempty"`
	Source        *gitopsSource    `json:"source,omitempty"`
	SourceIndex   int              `json:"sourceIndex"`
	Path          string           `json:"path"`
	Line          int              `json:"line"`
	DocumentIndex int              `json:"documentIndex"`
	Edit          *updateQuickEdit `json:"edit,omitempty"`
	Note          string           `json:"note,omitempty"`
	ValueFiles    []string         `json:"valueFiles,omitempty"`
}

const (
	updateEditRenderedNote = "rendered by a chart"
	updateEditByHandNote   = "installed without Argo CD or Flux: no file in Git declares it"
)

var errUpdateNoEdit = errors.New("nothing to edit: something already bumps it, a range sets it, or it has no newer version")

// updateEditEnv reads the cluster (or the demo): an app's sources, a source's files and an
// object's metadata (who applied it).
type updateEditEnv struct {
	sources func(ctx context.Context, app updateEditApp) (gitopsSources, error)
	files   func(ctx context.Context, src gitopsSource) (sourceFiles, error)
	meta    func(ctx context.Context, tool, kind, namespace, name string) (fluxMeta, error)
}

// UpdateEditTarget resolves where one ClusterUpdates item is edited. itemJSON is the item as
// ClusterUpdates gave it (owner, workloads, edit), plus "version": the candidate picked ("" for
// the edit's). → {"app":{tool,kind,namespace,name},"source","sourceIndex","path","line",
// "documentIndex","edit":{GitOpsQuickEdit's editJSON},"note","valueFiles"}. With no file found,
// note says why and edit is absent; an image a chart renders gives note "rendered by a chart",
// the chart's valueFiles and path the first of them (edit absent: the tag's key is not known).
// hostJSON (GitForgeTree's) reads files not served by a Flux artifact. kubeServer: see KubePods.
func UpdateEditTarget(configYAML, contextName, kubeServer, hostJSON, itemJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var item updateEditItem
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		return "", fmt.Errorf("item: %w", err)
	}

	revealUpdateItem(&item)

	contextName = unmaskContext(configYAML, contextName)
	if isDemoContext(configYAML, contextName) {
		res, err := resolveUpdateEdit(context.Background(), demoUpdateEditEnv(), item)
		if err != nil {
			return "", err
		}

		return toJSON(res)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitopsFindTimeout)
	defer cancel()

	target := kubeTarget{configYAML, contextName, kubeServer}

	res, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (updateEditTarget, error) {
		return resolveUpdateEdit(ctx, kubeUpdateEditEnv(k, target, hostJSON), item)
	})
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

// revealUpdateItem undoes screenshot masking of the names the apps send back.
func revealUpdateItem(item *updateEditItem) {
	if o := item.Owner; o != nil {
		o.Namespace, o.Name = privacy.reveal(strings.TrimSpace(o.Namespace)), privacy.reveal(strings.TrimSpace(o.Name))
	}

	for i := range item.Workloads {
		w := &item.Workloads[i]
		w.Namespace, w.Name = privacy.reveal(strings.TrimSpace(w.Namespace)), privacy.reveal(strings.TrimSpace(w.Name))
	}

	if e := item.Edit; e != nil && e.Name != "" {
		e.Name = privacy.reveal(strings.TrimSpace(e.Name))
	}
}

func kubeUpdateEditEnv(k *kubeClient, target kubeTarget, hostJSON string) updateEditEnv {
	return updateEditEnv{
		sources: func(ctx context.Context, app updateEditApp) (gitopsSources, error) {
			if app.Tool == "argocd" {
				return readArgoSources(ctx, k, app.Namespace, app.Name)
			}

			return readFluxSources(ctx, k, app.Kind, app.Namespace, app.Name)
		},
		files: func(ctx context.Context, src gitopsSource) (sourceFiles, error) {
			return readSourceFiles(ctx, k, target, hostJSON, src)
		},
		meta: func(ctx context.Context, tool, kind, namespace, name string) (fluxMeta, error) {
			return readUpdateObjectMeta(ctx, k, tool, kind, namespace, name)
		},
	}
}

// readUpdateObjectMeta reads the metadata of an Argo CD Application or a Flux object.
func readUpdateObjectMeta(ctx context.Context, k *kubeClient, tool, kind, namespace, name string) (fluxMeta, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return fluxMeta{}, err
	}

	var p string

	if tool == "argocd" {
		version, ok := groups[groupArgo]
		if !ok {
			return fluxMeta{}, errors.New("the cluster runs no Argo CD")
		}

		p = "/apis/" + groupArgo + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/applications/" + url.PathEscape(name)
	} else if p, err = fluxPath(groups, kind, namespace, name); err != nil {
		return fluxMeta{}, err
	}

	var o struct {
		Metadata fluxMeta `json:"metadata"`
	}
	if err := k.get(ctx, p, &o); err != nil {
		return fluxMeta{}, err
	}

	return o.Metadata, nil
}

func demoUpdateEditEnv() updateEditEnv {
	return updateEditEnv{
		sources: func(_ context.Context, app updateEditApp) (gitopsSources, error) {
			return demoGitOpsSource(app.Tool), nil
		},
		files: func(_ context.Context, src gitopsSource) (sourceFiles, error) {
			return treeFilesUnder(demoFluxTree().files, src.Path), nil
		},
		// The demo's charts are applied by the flux-system Kustomization, whose source does
		// not hold them: their edit says so.
		meta: func(_ context.Context, _, _, namespace, name string) (fluxMeta, error) {
			return fluxMeta{Name: name, Namespace: namespace, Labels: map[string]string{
				fluxKustomizeNameLabel: "flux-system", fluxKustomizeNsLabel: "flux-system",
			}}, nil
		},
	}
}

func resolveUpdateEdit(ctx context.Context, env updateEditEnv, item updateEditItem) (updateEditTarget, error) {
	e := item.Edit
	if e == nil {
		return updateEditTarget{}, errUpdateNoEdit
	}

	o := item.Owner
	if o == nil || o.Tool != "flux" && o.Tool != "argocd" {
		return updateEditTarget{Note: updateEditByHandNote}, nil
	}

	owner := updateEditApp{Tool: o.Tool, Kind: o.Kind, Namespace: o.Namespace, Name: o.Name}

	switch {
	case e.Op == "image":
		return resolveImageEdit(ctx, env, owner, item)
	case e.Op == "path" && e.Path == "":
		return resolveRenderedEdit(ctx, env, owner, e)
	case e.Op == "path":
		return resolveChartEdit(ctx, env, owner, item)
	}

	return updateEditTarget{}, fmt.Errorf("unknown edit %q", e.Op)
}

// resolveImageEdit finds the workload's manifest in its owner's sources; a kustomization's
// images: entry for the image wins over the workload's line (kustomize rewrites it).
func resolveImageEdit(ctx context.Context, env updateEditEnv, owner updateEditApp, item updateEditItem) (updateEditTarget, error) {
	e := item.Edit
	if len(item.Workloads) == 0 {
		return updateEditTarget{}, errors.New("the item names no workload")
	}

	if owner.Tool == "flux" && owner.Kind != "Kustomization" {
		return updateEditTarget{Note: updateEditRenderedNote}, nil
	}

	repo := cmp.Or(item.Repo, imageRepo(e.Image))
	tag := cmp.Or(item.Version, imageTag(e.Image))
	q := manifestQuery{kind: e.Kind, namespace: item.Workloads[0].Namespace, name: e.Name}

	return findUpdateEdit(ctx, env, owner, q, func(files map[string][]byte, m manifestMatch) (string, updateQuickEdit) {
		qe := updateQuickEdit{Op: "image", DocumentIndex: m.DocumentIndex, Kind: e.Kind, Name: e.Name, Container: e.Container, Image: repo + ":" + tag}

		written := writtenImage(files[m.Path], m.DocumentIndex, e.Container)
		if p, ki, ok := kustomizeImageEntry(files, path.Dir(m.Path), imageRepo(written)); ok {
			return p, updateQuickEdit{Op: "image", DocumentIndex: ki.DocumentIndex, KustomizeImage: ki.Name, Image: tag}
		}

		// Keep the image as the file writes it ("podinfo" rather than docker.io/library/…).
		if written != "" {
			qe.Image = imageRepo(written) + ":" + tag
		}

		return m.Path, qe
	})
}

// resolveRenderedEdit points at the first values file of the chart that renders the image:
// the key holding its tag is the chart's, not known here.
func resolveRenderedEdit(ctx context.Context, env updateEditEnv, owner updateEditApp, e *updateEdit) (updateEditTarget, error) {
	res := updateEditTarget{App: &owner, Note: updateEditRenderedNote, ValueFiles: e.ValueFiles}

	sources, err := env.sources(ctx, owner)
	if err != nil {
		return updateEditTarget{}, err
	}

	for i := range sources.Sources {
		if len(res.ValueFiles) == 0 {
			res.ValueFiles = sources.Sources[i].ValueFiles
		}
	}

	if len(res.ValueFiles) == 0 {
		return res, nil
	}

	idx, p := valueFileSource(sources.Sources, res.ValueFiles[0])
	if idx >= 0 {
		src := sources.Sources[idx]
		res.Source, res.SourceIndex, res.Path = &src, idx, p
	}

	return res, nil
}

// valueFileSource is the source a values file is read from and its path in that repository:
// Argo CD's "$ref/values.yaml" names a source by its ref, else the file is in the chart's.
func valueFileSource(sources []gitopsSource, file string) (int, string) {
	if rest, ok := strings.CutPrefix(file, "$"); ok {
		_, p, _ := strings.Cut(rest, "/")

		i := slices.IndexFunc(sources, func(s gitopsSource) bool { return s.Kind == gitopsKindValues })

		return i, p
	}

	if len(sources) == 0 {
		return -1, ""
	}

	return 0, path.Clean(file)
}

// resolveChartEdit finds the HelmRelease or Application (or the OCIRepository a HelmRelease
// takes its chart from) in the sources of the app that applied it.
func resolveChartEdit(ctx context.Context, env updateEditEnv, owner updateEditApp, item updateEditItem) (updateEditTarget, error) {
	e := item.Edit
	kind, name := cmp.Or(e.Kind, owner.Kind), cmp.Or(e.Name, owner.Name)

	meta, err := env.meta(ctx, owner.Tool, kind, owner.Namespace, name)
	if err != nil {
		return updateEditTarget{}, err
	}

	value := cmp.Or(item.Version, e.Value)
	q := manifestQuery{kind: kind, namespace: owner.Namespace, name: name}

	for _, parent := range declaringApps(owner.Tool, meta) {
		res, err := findUpdateEdit(ctx, env, parent, q, func(_ map[string][]byte, m manifestMatch) (string, updateQuickEdit) {
			return m.Path, updateQuickEdit{Op: "path", DocumentIndex: m.DocumentIndex, Kind: kind, Name: name, Path: e.Path, Value: value}
		})
		if err == nil && res.Edit != nil {
			return res, nil
		}
	}

	return updateEditTarget{Note: fmt.Sprintf("no Argo CD or Flux app declares %s %s in Git", kind, name)}, nil
}

// declaringApps are the apps that may have applied an object, from its labels: the Flux
// Kustomization, or the parent Argo CD Application of its tracking.
func declaringApps(tool string, m fluxMeta) []updateEditApp {
	if tool == "flux" {
		name := m.Labels[fluxKustomizeNameLabel]
		if name == "" {
			return nil
		}

		return []updateEditApp{{Tool: "flux", Kind: "Kustomization", Namespace: cmp.Or(m.Labels[fluxKustomizeNsLabel], m.Namespace), Name: name}}
	}

	ref := m.Labels[argoInstanceLabel]
	if id := m.Annotations[argoTrackingID]; id != "" {
		ref, _, _ = strings.Cut(id, ":")
	}

	if ref == "" {
		return nil
	}

	var out []updateEditApp

	for _, key := range argoAppKeys(ref, m.Namespace) {
		ns, name, _ := strings.Cut(key, "/")
		if ns != m.Namespace || name != m.Name {
			out = append(out, updateEditApp{Tool: "argocd", Kind: "Application", Namespace: ns, Name: name})
		}
	}

	return out
}

// findUpdateEdit looks for q in every source of app; edit makes the quick edit of the first
// match (the file it applies to and the edit), which is checked on the file for its line.
func findUpdateEdit(ctx context.Context, env updateEditEnv, app updateEditApp, q manifestQuery, edit func(map[string][]byte, manifestMatch) (string, updateQuickEdit)) (updateEditTarget, error) {
	sources, err := env.sources(ctx, app)
	if err != nil {
		return updateEditTarget{}, err
	}

	note := cmp.Or(sources.Note, "the app has no source")

	for i, src := range sources.Sources {
		var files map[string][]byte

		found, err := findInSource(ctx, src, q, func(ctx context.Context) (sourceFiles, error) {
			sf, err := env.files(ctx, src)
			files = sf.files

			return sf, err
		})
		if err != nil {
			return updateEditTarget{}, err
		}

		if len(found.Matches) == 0 {
			note = found.Note

			continue
		}

		m := found.Matches[0]
		p, qe := edit(files, m)
		res := updateEditTarget{App: &app, Source: &src, SourceIndex: i, Path: p, Line: m.Line, DocumentIndex: qe.DocumentIndex, Edit: &qe, Note: found.Note}

		if line, err := checkUpdateEdit(files[p], qe); err != nil {
			res.Note = err.Error()
		} else if line > 0 {
			res.Line = line
		}

		return res, nil
	}

	return updateEditTarget{App: &app, Note: note}, nil
}

// checkUpdateEdit applies qe to content and returns the line it changes.
func checkUpdateEdit(content []byte, qe updateQuickEdit) (int, error) {
	e := quickEdit{
		Op: qe.Op, DocumentIndex: qe.DocumentIndex, Kind: qe.Kind, Name: qe.Name, Container: qe.Container,
		KustomizeImage: qe.KustomizeImage, Image: qe.Image, Path: qe.Path,
	}
	if qe.Op == "path" {
		b, err := json.Marshal(qe.Value)
		if err != nil {
			return 0, err
		}

		e.Value = b
	}

	res, err := gitOpsQuickEdit(string(content), e)
	if err != nil {
		return 0, err
	}

	return res.Line, nil
}

// writtenImage is the image the file sets on container of document doc, "" when not found.
func writtenImage(content []byte, doc int, container string) string {
	docs, err := parseQuickEditDocs(string(content))
	if err != nil {
		return ""
	}

	for _, d := range quickEditTargetsOf(docs).Documents {
		if d.Index != doc {
			continue
		}

		for _, c := range d.Containers {
			if c.Name == container {
				return c.Image
			}
		}
	}

	return ""
}

// kustomizeImageEntry finds an images: entry naming repo in the kustomization files of dir
// and of its parents, nearest first.
func kustomizeImageEntry(files map[string][]byte, dir, repo string) (string, quickEditKustImage, bool) {
	if repo == "" {
		return "", quickEditKustImage{}, false
	}

	for d := dir; ; d = path.Dir(d) {
		for _, base := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization"} {
			p := path.Join(d, base)

			content, ok := files[p]
			if !ok {
				continue
			}

			docs, err := parseQuickEditDocs(string(content))
			if err != nil {
				continue
			}

			for _, ki := range quickEditTargetsOf(docs).KustomizeImages {
				if ki.Name == repo && ki.Digest == "" {
					return p, ki, true
				}
			}
		}

		if d == "." || d == "/" || d == "" {
			return "", quickEditKustImage{}, false
		}
	}
}

// imageTag is the tag of an image reference, "" when it has none.
func imageTag(image string) string {
	ref := strings.SplitN(image, "@", 2)[0]
	if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
		return ref[i+1:]
	}

	return ""
}
