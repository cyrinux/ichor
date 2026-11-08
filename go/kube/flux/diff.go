package flux

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/kubediff"
)

// The Flux diff: what reconciling a Kustomization now would change, like `flux diff
// kustomization`, without the flux CLI or a checkout. The source's artifact is read in the
// cluster, built as kustomize-controller builds it, and each object goes to the API server
// as a server-side apply dry run. Read only.

const (
	// DiffTimeout bounds a diff.
	DiffTimeout    = 90 * time.Second
	diffParallel   = 8
	diffMaxObjects = 2000
	// fieldManager is the field manager kustomize-controller applies with: the dry run
	// takes its fields, so what it does not own (a field set by hand) stays, as it would.
	fieldManager = "kustomize-controller"

	// What kustomize-controller leaves alone: reconcile disabled, or create only.
	reconcileAnnotation = "kustomize.toolkit.fluxcd.io/reconcile"
	ssaAnnotation       = "kustomize.toolkit.fluxcd.io/ssa"
	pruneAnnotation     = "kustomize.toolkit.fluxcd.io/prune"
)

var (
	// ErrDiffKind refuses a diff of anything else than a Kustomization.
	ErrDiffKind = errors.New("only a Kustomization can be compared with its source yet")
	// varName is what kustomize-controller accepts as a postBuild variable name.
	varName = regexp.MustCompile(`^[_[:alpha:]][_[:alpha:][:digit:]]*$`)
)

// diffObject is the Kustomization as the diff reads it.
type diffObject struct {
	Metadata Meta          `json:"metadata"`
	Spec     KustomizeSpec `json:"spec"`
	Status   struct {
		LastAppliedRevision string `json:"lastAppliedRevision"`
		Inventory           *struct {
			Entries []struct {
				ID      string `json:"id"`
				Version string `json:"v"`
			} `json:"entries"`
		} `json:"inventory"`
	} `json:"status"`
}

// Diff compares the Kustomization namespace/name with what its source would apply now: per
// object created, changed (with a unified diff), deleted (pruned), unchanged, encrypted (SOPS,
// not compared), ignored or error. Secret values never show.
func Diff(ctx context.Context, k Env, namespace, name string) (kubediff.Result, error) {
	groups, err := kube.APIGroups(ctx, k.Client)
	if err != nil {
		return kubediff.Result{}, err
	}

	ksPath, err := Path(groups, "Kustomization", namespace, name)
	if err != nil {
		return kubediff.Result{}, err
	}

	var ks diffObject
	if err := k.Get(ctx, ksPath, &ks); err != nil {
		return kubediff.Result{}, err
	}

	ref := ks.Spec.SourceRef
	if ref.Kind == "" || ref.Kind == "HelmChart" || ref.Kind == "HelmRepository" {
		return kubediff.Result{}, fmt.Errorf("unsupported source kind %q", ref.Kind)
	}

	srcPath, err := Path(groups, ref.Kind, cmp.Or(ref.Namespace, namespace), ref.Name)
	if err != nil {
		return kubediff.Result{}, err
	}

	var src struct {
		Status struct {
			Artifact *Artifact `json:"artifact"`
		} `json:"status"`
	}
	if err := k.Get(ctx, srcPath, &src); err != nil {
		return kubediff.Result{}, fmt.Errorf("%s %s: %w", ref.Kind, ref.Name, err)
	}

	tgz, err := FetchArtifact(ctx, k, src.Status.Artifact)
	if err != nil {
		return kubediff.Result{}, err
	}

	vars, masker, err := postBuildVars(ctx, k, ks.Spec, namespace)
	if err != nil {
		return kubediff.Result{}, err
	}

	built, err := buildArtifact(tgz, ks.Spec, name, namespace, vars)
	if err != nil {
		return kubediff.Result{}, err
	}

	if len(built) > diffMaxObjects {
		return kubediff.Result{}, fmt.Errorf("the Kustomization builds %d objects, more than the app compares (%d)", len(built), diffMaxObjects)
	}

	out := kubediff.Result{
		Kind: "Kustomization", Namespace: namespace, Name: name,
		Revision: src.Status.Artifact.Revision, Applied: ks.Status.LastAppliedRevision,
		Warnings: []string{},
	}

	if ks.Spec.Suspend {
		out.Warnings = append(out.Warnings, "The Kustomization is suspended: nothing is applied until it is resumed.")
	}

	index := kubediff.NewIndex(k.Client)
	out.Resources = diffBuilt(ctx, k, index, built, masker)

	if ks.Spec.Prune && ks.Status.Inventory != nil {
		rendered := map[string]bool{}
		for _, r := range out.Resources {
			rendered[r.Namespace+"_"+r.Name+"_"+r.Group+"_"+r.Kind] = true
		}

		for _, e := range ks.Status.Inventory.Entries {
			if rendered[e.ID] {
				continue
			}

			if r, ok := diffPruned(ctx, k, index, e.ID, e.Version, masker); ok {
				out.Resources = append(out.Resources, r)
			}
		}
	}

	kubediff.Sort(out.Resources)

	return out, nil
}

// postBuildVars are the postBuild variables (substituteFrom in order, then substitute,
// newlines removed as the controller does), nil without postBuild, with a masker for the
// values read from Secrets.
func postBuildVars(ctx context.Context, k Env, spec KustomizeSpec, namespace string) (map[string]string, *kubediff.Masker, error) {
	masker := kubediff.NewMasker()
	if spec.PostBuild == nil {
		return nil, masker, nil
	}

	vars := map[string]string{}

	for _, from := range spec.PostBuild.SubstituteFrom {
		var obj struct {
			Data map[string]string `json:"data"`
		}

		plural := map[string]string{"ConfigMap": "configmaps", "Secret": "secrets"}[from.Kind]
		if plural == "" {
			return nil, nil, fmt.Errorf("postBuild: unsupported kind %q", from.Kind)
		}

		err := k.Get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/"+plural+"/"+url.PathEscape(from.Name), &obj)
		if err != nil {
			if kube.IsNotFound(err) && from.Optional {
				continue
			}

			return nil, nil, fmt.Errorf("postBuild %s %s: %w", from.Kind, from.Name, err)
		}

		for key, v := range obj.Data {
			if from.Kind == "Secret" {
				d, err := base64.StdEncoding.DecodeString(v)
				if err != nil {
					continue
				}

				masker.Add(string(d))
				v = strings.ReplaceAll(string(d), "\n", "")
				masker.Add(v)
			}

			vars[key] = strings.ReplaceAll(v, "\n", "")
		}
	}

	for key, v := range spec.PostBuild.Substitute {
		vars[key] = strings.ReplaceAll(v, "\n", "")
	}

	for key := range vars {
		if !varName.MatchString(key) {
			return nil, nil, fmt.Errorf("postBuild: variable name %q is invalid, it must match %s", key, varName)
		}
	}

	return vars, masker, nil
}

// diffBuilt compares each built object with the cluster, diffParallel at a time.
func diffBuilt(ctx context.Context, k Env, index *kubediff.Index, built []Built, masker *kubediff.Masker) []kubediff.Resource {
	out := make([]kubediff.Resource, len(built))
	sem := make(chan struct{}, diffParallel)

	var wg sync.WaitGroup

	for i, b := range built {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			out[i] = diffOne(ctx, k, index, b, masker)
		})
	}

	wg.Wait()

	return out
}

func diffOne(ctx context.Context, k Env, index *kubediff.Index, b Built, masker *kubediff.Masker) kubediff.Resource {
	apiVersion, _ := b.Obj["apiVersion"].(string)
	kind, _ := b.Obj["kind"].(string)
	meta, _ := b.Obj["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	namespace, _ := meta["namespace"].(string)

	group, version := kubediff.SplitAPIVersion(apiVersion)
	r := kubediff.Resource{Group: group, Version: version, Kind: kind, Namespace: namespace, Name: name}

	if b.Encrypted {
		r.Change = kubediff.ChangeEncrypted

		return r
	}

	fail := func(err error) kubediff.Resource {
		r.Change, r.Error = kubediff.ChangeError, masker.Mask(err.Error())

		return r
	}

	res, err := index.Resource(ctx, apiVersion, kind)
	if err != nil {
		return fail(err)
	}

	if res.Namespaced && namespace == "" {
		return fail(errors.New("namespaced object without a namespace"))
	}

	if !res.Namespaced {
		r.Namespace = ""
	}

	path, err := index.Path(ctx, apiVersion, kind, namespace, name)
	if err != nil {
		return fail(err)
	}

	if excluded(b.Obj) {
		r.Change = kubediff.ChangeIgnored

		return r
	}

	live, err := kubediff.ReadLive(ctx, k.Client, path)
	if err != nil {
		return fail(err)
	}

	if live != nil && (excluded(live) || strings.EqualFold(annotation(b.Obj, ssaAnnotation), "IfNotPresent")) {
		r.Change = kubediff.ChangeIgnored

		return r
	}

	wanted, err := kubediff.DryRunApply(ctx, k.Client, path, fieldManager, b.Obj)
	if err != nil {
		return fail(err)
	}

	kubediff.Objects(&r, live, wanted, masker)

	return r
}

// diffPruned is the inventory entry id that the build no longer has: deleted when it
// still exists and may be pruned.
func diffPruned(ctx context.Context, k Env, index *kubediff.Index, id, version string, masker *kubediff.Masker) (kubediff.Resource, bool) {
	e, ok := parseInventoryID(id)
	if !ok || version == "" {
		return kubediff.Resource{}, false
	}

	apiVersion := version
	if e.Group != "" {
		apiVersion = e.Group + "/" + version
	}

	r := kubediff.Resource{Group: e.Group, Version: version, Kind: e.Kind, Namespace: e.Namespace, Name: e.Name}

	path, err := index.Path(ctx, apiVersion, e.Kind, e.Namespace, e.Name)
	if err != nil {
		return kubediff.Resource{}, false
	}

	live, err := kubediff.ReadLive(ctx, k.Client, path)
	if err != nil {
		r.Change, r.Error = kubediff.ChangeError, masker.Mask(err.Error())

		return r, true
	}

	if live == nil {
		return kubediff.Resource{}, false
	}

	if strings.EqualFold(metaValue(live, pruneAnnotation), "disabled") || excluded(live) {
		r.Change = kubediff.ChangeIgnored

		return r, true
	}

	kubediff.Objects(&r, live, nil, masker)

	return r, true
}

// excluded is an object kustomize-controller never applies: reconcile disabled, or ssa
// Ignore (label or annotation).
func excluded(obj map[string]any) bool {
	return strings.EqualFold(metaValue(obj, reconcileAnnotation), "disabled") || strings.EqualFold(metaValue(obj, ssaAnnotation), "Ignore")
}

// metaValue is the annotation key of obj, else its label key.
func metaValue(obj map[string]any, key string) string {
	if v := annotation(obj, key); v != "" {
		return v
	}

	meta, _ := obj["metadata"].(map[string]any)
	labels, _ := meta["labels"].(map[string]any)
	v, _ := labels[key].(string)

	return v
}

func annotation(obj map[string]any, key string) string {
	meta, _ := obj["metadata"].(map[string]any)
	ann, _ := meta["annotations"].(map[string]any)
	v, _ := ann[key].(string)

	return v
}
