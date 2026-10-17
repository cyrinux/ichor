package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Quick edits rewrite one value of a manifest in Git (an image, the replicas, an explicit
// path) without touching any other byte of the file: the value's node is found with
// yaml.Node, then only its text is spliced back into the original content at its line and
// column, so comments, quoting, flow sequences and block scalars stay as they were and the
// review shows a one-line diff.

// quickEdit is the edit the app asks for (editJSON).
//
//	{"op":"image","documentIndex":0,"container":"app","image":"ghcr.io/example-org/app:1.2.3"}
//	{"op":"image","documentIndex":0,"kustomizeImage":"ghcr.io/example-org/app","image":"1.2.3"}
//	{"op":"replicas","documentIndex":0,"replicas":3}
//	{"op":"replicas","documentIndex":0,"kustomizeReplica":"app","replicas":3}
//	{"op":"path","documentIndex":0,"path":"spec.values.image.tag","value":"1.2.3"}
//
// kind and name, when set, say which object documentIndex meant: when the file moved since
// the targets were read, the one document of that kind and name is edited instead.
type quickEdit struct {
	Op               string          `json:"op"`
	DocumentIndex    int             `json:"documentIndex"`
	Kind             string          `json:"kind"`
	Name             string          `json:"name"`
	Container        string          `json:"container"`
	KustomizeImage   string          `json:"kustomizeImage"`
	KustomizeReplica string          `json:"kustomizeReplica"`
	Image            string          `json:"image"`
	Replicas         *int            `json:"replicas"`
	Path             string          `json:"path"`
	Value            json.RawMessage `json:"value"`
}

// quickEditResult is GitOpsQuickEdit's answer; line is 1-based (the first changed line,
// or the value's line when nothing changed).
type quickEditResult struct {
	Content string `json:"content"`
	Changed bool   `json:"changed"`
	Line    int    `json:"line"`
}

type quickEditTargets struct {
	Documents         []quickEditDocument    `json:"documents"`
	KustomizeImages   []quickEditKustImage   `json:"kustomizeImages"`
	KustomizeReplicas []quickEditKustReplica `json:"kustomizeReplicas"`
}

// quickEditDocument is a workload of the file; replicas is absent when the manifest does
// not set it (Kubernetes then runs 1), scalable tells whether the replicas op applies.
type quickEditDocument struct {
	Index      int                  `json:"index"`
	Kind       string               `json:"kind"`
	Name       string               `json:"name"`
	Replicas   *int                 `json:"replicas,omitempty"`
	Scalable   bool                 `json:"scalable"`
	Containers []quickEditContainer `json:"containers"`
}

// quickEditContainer: overriddenBy names the kustomize images entry of the same file that
// replaces this image at build time (edit that entry instead).
type quickEditContainer struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Init         bool   `json:"init,omitempty"`
	Line         int    `json:"line"`
	OverriddenBy string `json:"overriddenBy,omitempty"`
}

type quickEditKustImage struct {
	DocumentIndex int    `json:"documentIndex"`
	Name          string `json:"name"`
	NewName       string `json:"newName,omitempty"`
	NewTag        string `json:"newTag,omitempty"`
	Digest        string `json:"digest,omitempty"`
	Line          int    `json:"line"`
}

type quickEditKustReplica struct {
	DocumentIndex int    `json:"documentIndex"`
	Name          string `json:"name"`
	Count         *int   `json:"count,omitempty"`
	Line          int    `json:"line"`
}

// GitOpsQuickEdit rewrites one value of a manifest file (editJSON, see quickEdit) and returns
// {"content","changed","line"}. Pure: no network, no cluster.
func GitOpsQuickEdit(content, editJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	e, err := parseQuickEdit(editJSON)
	if err != nil {
		return "", err
	}

	res, err := gitOpsQuickEdit(content, e)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// GitOpsQuickEditTargets lists what a quick edit can change in a file: its workloads
// (containers, replicas) and its kustomize images and replicas entries.
func GitOpsQuickEditTargets(content string) (out string, err error) {
	defer maskResult(&out, &err)

	docs, err := parseQuickEditDocs(content)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(quickEditTargetsOf(docs))
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func parseQuickEdit(editJSON string) (quickEdit, error) {
	var e quickEdit

	dec := json.NewDecoder(strings.NewReader(editJSON))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&e); err != nil {
		return quickEdit{}, fmt.Errorf("the quick edit is not valid: %w", err)
	}

	if strings.TrimSpace(editJSON[dec.InputOffset():]) != "" {
		return quickEdit{}, errors.New("the quick edit is not valid: data after the JSON object")
	}

	if e.DocumentIndex < 0 {
		return quickEdit{}, errors.New("the quick edit is not valid: documentIndex is negative")
	}

	return e, nil
}

// parseQuickEditDocs decodes every document of the file; nil stands for an empty document,
// so indexes match the documents' order in the file.
func parseQuickEditDocs(content string) ([]*yaml.Node, error) {
	if len(content) > gitForgeMaxFile {
		return nil, errors.New("the file is over 1 MiB: edit it on a computer")
	}

	dec := yaml.NewDecoder(strings.NewReader(content))

	var docs []*yaml.Node

	for {
		var doc yaml.Node

		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}

		if err != nil {
			return nil, fmt.Errorf("the file is not plain YAML (a template?): %w", err)
		}

		var root *yaml.Node
		if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
			root = doc.Content[0]
		}

		docs = append(docs, root)
	}
}

func quickEditTargetsOf(docs []*yaml.Node) quickEditTargets {
	t := quickEditTargets{
		Documents:         []quickEditDocument{},
		KustomizeImages:   []quickEditKustImage{},
		KustomizeReplicas: []quickEditKustReplica{},
	}

	for i, root := range docs {
		t.KustomizeImages = append(t.KustomizeImages, kustImagesOf(i, root)...)
		t.KustomizeReplicas = append(t.KustomizeReplicas, kustReplicasOf(i, root)...)

		if d, ok := workloadOf(i, root); ok {
			t.Documents = append(t.Documents, d)
		}
	}

	for di := range t.Documents {
		for ci, c := range t.Documents[di].Containers {
			repo := imageRepo(c.Image)
			if k := slices.IndexFunc(t.KustomizeImages, func(k quickEditKustImage) bool { return k.Name == repo }); k >= 0 {
				t.Documents[di].Containers[ci].OverriddenBy = t.KustomizeImages[k].Name
			}
		}
	}

	return t
}

func workloadOf(i int, root *yaml.Node) (quickEditDocument, bool) {
	cs := containersOf(root)
	if len(cs) == 0 {
		return quickEditDocument{}, false
	}

	d := quickEditDocument{
		Index:      i,
		Kind:       scalarAt(root, "kind"),
		Name:       scalarAt(mapValue(root, "metadata"), "name"),
		Scalable:   isScalable(root),
		Containers: make([]quickEditContainer, 0, len(cs)),
	}

	if n, ok := intScalar(mapValue(mapValue(root, "spec"), "replicas")); ok {
		d.Replicas = &n
	}

	for _, c := range cs {
		qc := quickEditContainer{Name: c.name, Init: c.init}
		if c.image != nil && c.image.Kind == yaml.ScalarNode {
			qc.Image, qc.Line = c.image.Value, c.image.Line
		}

		d.Containers = append(d.Containers, qc)
	}

	return d, true
}

func kustImagesOf(i int, root *yaml.Node) []quickEditKustImage {
	var out []quickEditKustImage

	for _, entry := range seqItems(kustomizeList(root, "images")) {
		name := mapValue(entry, "name")
		if name == nil || name.Kind != yaml.ScalarNode {
			continue
		}

		out = append(out, quickEditKustImage{
			DocumentIndex: i,
			Name:          name.Value,
			NewName:       scalarAt(entry, "newName"),
			NewTag:        scalarAt(entry, "newTag"),
			Digest:        scalarAt(entry, "digest"),
			Line:          name.Line,
		})
	}

	return out
}

func kustReplicasOf(i int, root *yaml.Node) []quickEditKustReplica {
	var out []quickEditKustReplica

	for _, entry := range seqItems(kustomizeList(root, "replicas")) {
		name := mapValue(entry, "name")
		if name == nil || name.Kind != yaml.ScalarNode {
			continue
		}

		r := quickEditKustReplica{DocumentIndex: i, Name: name.Value, Line: name.Line}
		if n, ok := intScalar(mapValue(entry, "count")); ok {
			r.Count = &n
		}

		out = append(out, r)
	}

	return out
}

// kustomizeList is the images or replicas list of a kustomization.yaml (with or without
// apiVersion/kind), or the images list of a Flux Kustomization's spec.
func kustomizeList(root *yaml.Node, key string) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}

	api, kind := scalarAt(root, "apiVersion"), scalarAt(root, "kind")

	var list *yaml.Node

	switch {
	case strings.HasPrefix(api, "kustomize.toolkit.fluxcd.io/") && kind == "Kustomization" && key == "images":
		list = mapValue(mapValue(root, "spec"), key)
	case strings.HasPrefix(api, "kustomize.config.k8s.io/"), api == "" && kind == "":
		list = mapValue(root, key)
	}

	if list == nil || list.Kind != yaml.SequenceNode {
		return nil
	}

	return list
}

func seqItems(n *yaml.Node) []*yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}

	return n.Content
}

type containerRef struct {
	name  string
	image *yaml.Node
	init  bool
}

// podSpecOf is the pod spec of a Pod, of a CronJob (spec.jobTemplate.spec.template.spec) or
// of any object with a pod template (Deployment, StatefulSet, DaemonSet, Job, Rollout…).
func podSpecOf(root *yaml.Node) *yaml.Node {
	spec := mapValue(root, "spec")
	if scalarAt(root, "kind") == "Pod" {
		return spec
	}

	if job := mapValue(mapValue(spec, "jobTemplate"), "spec"); job != nil {
		return mapValue(mapValue(job, "template"), "spec")
	}

	return mapValue(mapValue(spec, "template"), "spec")
}

// containersOf lists the containers, then the init containers, of a workload.
func containersOf(root *yaml.Node) []containerRef {
	pod := podSpecOf(root)

	var out []containerRef

	for _, key := range []string{"containers", "initContainers"} {
		for _, c := range seqItems(mapValue(pod, key)) {
			if c.Kind != yaml.MappingNode {
				continue
			}

			out = append(out, containerRef{name: scalarAt(c, "name"), image: mapValue(c, "image"), init: key == "initContainers"})
		}
	}

	return out
}

var scalableKinds = []string{"Deployment", "StatefulSet", "ReplicaSet", "ReplicationController", "Rollout"}

func isScalable(root *yaml.Node) bool {
	return slices.Contains(scalableKinds, scalarAt(root, "kind")) || mapValue(mapValue(root, "spec"), "replicas") != nil
}

func intScalar(n *yaml.Node) (int, bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.ShortTag() != "!!int" {
		return 0, false
	}

	var v int
	if err := n.Decode(&v); err != nil {
		return 0, false
	}

	return v, true
}

// imageRepo is an image reference without its tag and digest.
func imageRepo(image string) string {
	if i := strings.IndexByte(image, '@'); i >= 0 {
		image = image[:i]
	}

	if i := strings.LastIndexByte(image, ':'); i > strings.LastIndexByte(image, '/') {
		image = image[:i]
	}

	return image
}

// isBareTag: "1.2.3" (a tag alone) rather than an image reference.
func isBareTag(s string) bool {
	return !strings.ContainsAny(s, ":/@")
}
