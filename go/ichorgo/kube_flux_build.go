package ichorgo

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/fluxcd/pkg/envsubst"
	"go.yaml.in/yaml/v4"
	"sigs.k8s.io/kustomize/api/konfig"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/provider"
	kustypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/resid"
	sigsyaml "sigs.k8s.io/yaml"
)

// The manifests a Kustomization would apply, built as kustomize-controller builds them: the
// path's kustomization.yaml (generated when there is none) edited with the Kustomization's
// own settings, then the postBuild substitutions, commonMetadata and the owner labels.

const (
	fluxSourceRoot = "/source"
	// fluxSubstitute set to "disabled" (label or annotation) keeps an object as written.
	fluxSubstitute = "kustomize.toolkit.fluxcd.io/substitute"
)

// fluxKustomizeSpec holds the fields of a Kustomization's spec the build reads.
type fluxKustomizeSpec struct {
	SourceRef       fluxCrossRef `json:"sourceRef"`
	Path            string       `json:"path"`
	Prune           bool         `json:"prune"`
	Suspend         bool         `json:"suspend"`
	TargetNamespace string       `json:"targetNamespace"`
	NamePrefix      string       `json:"namePrefix"`
	NameSuffix      string       `json:"nameSuffix"`
	CommonMetadata  *struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"commonMetadata"`
	Patches []struct {
		Patch  string `json:"patch"`
		Target *struct {
			Group              string `json:"group"`
			Version            string `json:"version"`
			Kind               string `json:"kind"`
			Name               string `json:"name"`
			Namespace          string `json:"namespace"`
			LabelSelector      string `json:"labelSelector"`
			AnnotationSelector string `json:"annotationSelector"`
		} `json:"target"`
	} `json:"patches"`
	Images                  []kustypes.Image `json:"images"`
	Components              []string         `json:"components"`
	IgnoreMissingComponents bool             `json:"ignoreMissingComponents"`
	PostBuild               *struct {
		// SubstituteStrategy "Always" substitutes even without variables (for the defaults).
		SubstituteStrategy string            `json:"substituteStrategy"`
		Substitute         map[string]string `json:"substitute"`
		SubstituteFrom     []struct {
			Kind     string `json:"kind"`
			Name     string `json:"name"`
			Optional bool   `json:"optional"`
		} `json:"substituteFrom"`
	} `json:"postBuild"`
}

// fluxBuilt is one object the build produced. Encrypted (SOPS) objects keep only their identity.
type fluxBuilt struct {
	obj       map[string]any
	encrypted bool
}

// buildFluxKustomization builds spec from the artifact unpacked under fluxSourceRoot in fs.
// vars are the postBuild variables, nil when the Kustomization has no postBuild.
func buildFluxKustomization(fsys filesys.FileSystem, spec fluxKustomizeSpec, name, namespace string, vars map[string]string) ([]fluxBuilt, error) {
	dir := path.Join(fluxSourceRoot, strings.TrimPrefix(path.Clean("/"+spec.Path), "/"))

	if !fsys.IsDir(dir) {
		return nil, fmt.Errorf("the path %q is not in the source", spec.Path)
	}

	if err := writeFluxKustomization(fsys, spec, dir); err != nil {
		return nil, err
	}

	opts := krusty.MakeDefaultOptions()
	opts.LoadRestrictions = kustypes.LoadRestrictionsNone

	resmap, err := krusty.MakeKustomizer(opts).Run(fsys, dir)
	if err != nil {
		if strings.Contains(err.Error(), "://") {
			return nil, fmt.Errorf("kustomize build: %w (remote bases are not fetched from the phone)", err)
		}

		return nil, fmt.Errorf("kustomize build: %w", err)
	}

	out := make([]fluxBuilt, 0, resmap.Size())

	for _, r := range resmap.Resources() {
		y, err := r.AsYAML()
		if err != nil {
			return nil, err
		}

		b, err := fluxPostBuild(y, vars, spec, name, namespace)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", r.GetKind(), r.GetName(), err)
		}

		out = append(out, b)
	}

	return out, nil
}

// writeFluxKustomization edits the path's kustomization file (generated when there is none)
// with the Kustomization's settings, as kustomize-controller does (fluxcd/pkg/kustomize
// Generator.GenerateManifest, ported: that package pulls in controller-runtime).
func writeFluxKustomization(fsys filesys.FileSystem, spec fluxKustomizeSpec, dir string) error {
	data, kfile, generated, err := findOrGenerateKustomization(fsys, dir)
	if err != nil {
		return err
	}

	kus := kustypes.Kustomization{TypeMeta: kustypes.TypeMeta{APIVersion: kustypes.KustomizationVersion, Kind: kustypes.KustomizationKind}}
	if err := sigsyaml.Unmarshal(data, &kus); err != nil {
		return fmt.Errorf("%s: %w", path.Base(kfile), err)
	}

	if !generated && len(kus.Resources) == 0 {
		// Not empty for kustomize, which refuses an empty kustomization.
		kus.BuildMetadata = []string{"originAnnotations"}
	}

	if spec.TargetNamespace != "" {
		kus.Namespace = spec.TargetNamespace
	}

	if spec.NamePrefix != "" {
		kus.NamePrefix = spec.NamePrefix
	}

	if spec.NameSuffix != "" {
		kus.NameSuffix = spec.NameSuffix
	}

	for _, p := range spec.Patches {
		patch := kustypes.Patch{Patch: p.Patch}
		if t := p.Target; t != nil {
			patch.Target = &kustypes.Selector{
				ResId:              resid.ResId{Gvk: resid.Gvk{Group: t.Group, Version: t.Version, Kind: t.Kind}, Name: t.Name, Namespace: t.Namespace},
				LabelSelector:      t.LabelSelector,
				AnnotationSelector: t.AnnotationSelector,
			}
		}

		kus.Patches = append(kus.Patches, patch)
	}

	for _, c := range spec.Components {
		if !isLocalRelativePath(c) {
			return fmt.Errorf("component path %q must be local and relative", c)
		}

		if spec.IgnoreMissingComponents && !fsys.Exists(path.Join(dir, c)) {
			continue
		}

		kus.Components = append(kus.Components, c)
	}

	for _, image := range spec.Images {
		i := slices.IndexFunc(kus.Images, func(x kustypes.Image) bool { return x.Name == image.Name })
		if i < 0 {
			kus.Images = append(kus.Images, kustypes.Image{Name: image.Name, NewName: image.NewName, NewTag: image.NewTag, Digest: image.Digest})

			continue
		}

		// Fields merge like an overlay; tag and digest go together (setting one clears the other).
		if image.NewName != "" {
			kus.Images[i].NewName = image.NewName
		}

		if image.NewTag != "" || image.Digest != "" {
			kus.Images[i].NewTag, kus.Images[i].Digest = image.NewTag, image.Digest
		}
	}

	out, err := sigsyaml.Marshal(kus)
	if err != nil {
		return err
	}

	return fsys.WriteFile(kfile, out)
}

// findOrGenerateKustomization is the path's kustomization file and its content, or, when
// it has none (generated), the one kustomize-controller generates: every manifest below it,
// and each subdirectory that has its own kustomization file.
func findOrGenerateKustomization(fsys filesys.FileSystem, dir string) (data []byte, kfile string, generated bool, err error) {
	for _, f := range konfig.RecognizedKustomizationFileNames() {
		if p := path.Join(dir, f); fsys.Exists(p) && !fsys.IsDir(p) {
			data, err := fsys.ReadFile(p)

			return data, p, false, err
		}
	}

	rf := provider.NewDefaultDepProvider().GetResourceFactory()
	resources := []string{}

	err = fsys.Walk(dir, func(p string, info fs.FileInfo, err error) (walkErr error) {
		if err != nil || p == dir {
			return err
		}

		if info.IsDir() {
			for _, f := range konfig.RecognizedKustomizationFileNames() {
				if k := path.Join(p, f); fsys.Exists(k) && !fsys.IsDir(k) {
					resources = append(resources, "."+strings.TrimPrefix(p, dir))

					return fs.SkipDir
				}
			}

			return nil
		}

		if ext := path.Ext(p); ext != ".yaml" && ext != ".yml" {
			return nil
		}

		b, err := fsys.ReadFile(p)
		if err != nil {
			return err
		}

		// kustomize's parser can panic on odd input: an error, not a crash.
		defer func() {
			if r := recover(); r != nil {
				walkErr = fmt.Errorf("parse %s: %v", path.Base(p), r)
			}
		}()

		if _, err := rf.SliceFromBytes(b); err != nil {
			return fmt.Errorf("decode Kubernetes YAML from %s: %w", strings.TrimPrefix(p, fluxSourceRoot+"/"), err)
		}

		resources = append(resources, "."+strings.TrimPrefix(p, dir))

		return nil
	})
	if err != nil {
		return nil, "", true, err
	}

	kus := map[string]any{"apiVersion": kustypes.KustomizationVersion, "kind": kustypes.KustomizationKind}
	if len(resources) == 0 {
		kus["namespace"] = "_placeholder" // not empty for kustomize
	} else {
		kus["resources"] = resources
	}

	data, err = sigsyaml.Marshal(kus)

	return data, path.Join(dir, konfig.DefaultKustomizationFileName()), true, err
}

// isLocalRelativePath is fluxcd/pkg/kustomize's check that a component is not remote.
func isLocalRelativePath(p string) bool {
	lower := strings.ToLower(p)
	for _, prefix := range []string{"git::", "gh:", "ssh://", "https://", "http://", "git@", "github.com:", "github.com/"} {
		if len(prefix) < len(p) && strings.HasPrefix(lower, prefix) {
			return false
		}
	}

	return !path.IsAbs(p) && !path.IsAbs(strings.TrimPrefix(lower, "file://"))
}

// fluxPostBuild substitutes vars in one built object (unless it opts out), then sets
// commonMetadata and the labels kustomize-controller marks what it applies with, on the
// object's own metadata only, as the controller does after the build.
func fluxPostBuild(y []byte, vars map[string]string, spec fluxKustomizeSpec, name, namespace string) (fluxBuilt, error) {
	var obj map[string]any
	if err := yaml.Unmarshal(y, &obj); err != nil {
		return fluxBuilt{}, err
	}

	if _, ok := obj["sops"]; ok {
		delete(obj, "sops")
		delete(obj, "data")
		delete(obj, "stringData")
		delete(obj, "spec")

		return fluxBuilt{obj: obj, encrypted: true}, nil
	}

	always := spec.PostBuild != nil && spec.PostBuild.SubstituteStrategy == "Always"
	if (len(vars) > 0 || always) && !substitutionDisabled(obj) {
		// Not strict, the controller's default: a variable without a value is empty.
		s, err := envsubst.Eval(string(y), func(k string) (string, bool) { return vars[k], true })
		if err != nil {
			return fluxBuilt{}, fmt.Errorf("variable substitution: %w", err)
		}

		obj = nil
		if err := yaml.Unmarshal([]byte(s), &obj); err != nil {
			return fluxBuilt{}, fmt.Errorf("variable substitution: %w", err)
		}
	}

	meta, _ := obj["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		obj["metadata"] = meta
	}

	labels, _ := meta["labels"].(map[string]any)
	if labels == nil {
		labels = map[string]any{}
		meta["labels"] = labels
	}

	annotations, _ := meta["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
	}

	if m := spec.CommonMetadata; m != nil {
		for k, v := range m.Labels {
			labels[k] = v
		}

		for k, v := range m.Annotations {
			annotations[k] = v
		}
	}

	if len(annotations) > 0 {
		meta["annotations"] = annotations
	}

	labels[fluxOwnerName] = name
	labels[fluxOwnerNamespace] = namespace

	return fluxBuilt{obj: obj}, nil
}

func substitutionDisabled(obj map[string]any) bool {
	meta, _ := obj["metadata"].(map[string]any)

	for _, key := range []string{"labels", "annotations"} {
		if m, ok := meta[key].(map[string]any); ok && m[fluxSubstitute] == "disabled" {
			return true
		}
	}

	return false
}
