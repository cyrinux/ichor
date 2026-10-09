package ichorgo

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// The Flux diff: what reconciling a Kustomization now would change, like `flux diff
// kustomization`, without the flux CLI or a checkout. The source's artifact is read in the
// cluster, built as kustomize-controller builds it, and each object goes to the API server
// as a server-side apply dry run (the Linear plan document "D6. Flux", phase 5). Read only.

const (
	fluxDiffTimeout    = 90 * time.Second
	fluxDiffParallel   = 8
	fluxDiffMaxObjects = 2000
	// fluxFieldManager is the field manager kustomize-controller applies with: the dry run
	// takes its fields, so what it does not own (a field set by hand) stays, as it would.
	fluxFieldManager = "kustomize-controller"

	// What kustomize-controller leaves alone: reconcile disabled, or create only.
	fluxReconcileAnnotation = "kustomize.toolkit.fluxcd.io/reconcile"
	fluxSSAAnnotation       = "kustomize.toolkit.fluxcd.io/ssa"
	fluxPruneAnnotation     = "kustomize.toolkit.fluxcd.io/prune"
)

var (
	errFluxDiffKind = errors.New("only a Kustomization can be compared with its source yet")
	// fluxVarName is what kustomize-controller accepts as a postBuild variable name.
	fluxVarName = regexp.MustCompile(`^[_[:alpha:]][_[:alpha:][:digit:]]*$`)
)

// fluxDiff is the Flux shape of a GitOps diff.
type fluxDiff = gitOpsDiff

// fluxDiffObject is the Kustomization as the diff reads it.
type fluxDiffObject struct {
	Metadata fluxMeta          `json:"metadata"`
	Spec     fluxKustomizeSpec `json:"spec"`
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

// KubeFluxDiff compares the Flux object kind namespace/name with what its source would
// apply now (os:admin): per object created, changed (with a unified diff), deleted (pruned),
// unchanged, encrypted (SOPS, not compared), ignored or error. Secret values never show.
// Only kind Kustomization yet. kubeServer: see KubePods.
func KubeFluxDiff(configYAML, contextName, kubeServer, kind, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if kind != "Kustomization" {
		return "", errFluxDiffKind
	}

	if err := validateKubeName("kustomization", namespace, name); err != nil {
		return "", err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return toJSON(demoFluxDiff(namespace, name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), fluxDiffTimeout)
	defer cancel()

	res, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (fluxDiff, error) {
		return diffFluxKustomization(ctx, k, namespace, name)
	})
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

func diffFluxKustomization(ctx context.Context, k *kubeClient, namespace, name string) (fluxDiff, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return fluxDiff{}, err
	}

	ksPath, err := fluxPath(groups, "Kustomization", namespace, name)
	if err != nil {
		return fluxDiff{}, err
	}

	var ks fluxDiffObject
	if err := k.get(ctx, ksPath, &ks); err != nil {
		return fluxDiff{}, err
	}

	ref := ks.Spec.SourceRef
	if ref.Kind == "" || ref.Kind == "HelmChart" || ref.Kind == "HelmRepository" {
		return fluxDiff{}, fmt.Errorf("unsupported source kind %q", ref.Kind)
	}

	srcPath, err := fluxPath(groups, ref.Kind, cmp.Or(ref.Namespace, namespace), ref.Name)
	if err != nil {
		return fluxDiff{}, err
	}

	var src struct {
		Status struct {
			Artifact *fluxArtifact `json:"artifact"`
		} `json:"status"`
	}
	if err := k.get(ctx, srcPath, &src); err != nil {
		return fluxDiff{}, fmt.Errorf("%s %s: %w", ref.Kind, ref.Name, err)
	}

	tgz, err := fetchFluxArtifact(ctx, k, src.Status.Artifact)
	if err != nil {
		return fluxDiff{}, err
	}

	fsys := filesys.MakeFsInMemory()
	if err := untarFluxArtifact(tgz, fsys, fluxSourceRoot); err != nil {
		return fluxDiff{}, err
	}

	vars, masker, err := fluxPostBuildVars(ctx, k, ks.Spec, namespace)
	if err != nil {
		return fluxDiff{}, err
	}

	built, err := buildFluxKustomization(fsys, ks.Spec, name, namespace, vars)
	if err != nil {
		return fluxDiff{}, err
	}

	if len(built) > fluxDiffMaxObjects {
		return fluxDiff{}, fmt.Errorf("the Kustomization builds %d objects, more than the app compares (%d)", len(built), fluxDiffMaxObjects)
	}

	out := fluxDiff{
		Kind: "Kustomization", Namespace: namespace, Name: name,
		Revision: src.Status.Artifact.Revision, Applied: ks.Status.LastAppliedRevision,
		Warnings: []string{},
	}

	if ks.Spec.Suspend {
		out.Warnings = append(out.Warnings, "The Kustomization is suspended: nothing is applied until it is resumed.")
	}

	index := newKubeResourceIndex(k)
	out.Resources = diffFluxBuilt(ctx, k, index, built, masker)

	if ks.Spec.Prune && ks.Status.Inventory != nil {
		rendered := map[string]bool{}
		for _, r := range out.Resources {
			rendered[r.Namespace+"_"+r.Name+"_"+r.Group+"_"+r.Kind] = true
		}

		for _, e := range ks.Status.Inventory.Entries {
			if rendered[e.ID] {
				continue
			}

			if r, ok := diffFluxPruned(ctx, k, index, e.ID, e.Version, masker); ok {
				out.Resources = append(out.Resources, r)
			}
		}
	}

	sortDiffResources(out.Resources)

	return out, nil
}

// fluxPostBuildVars are the postBuild variables (substituteFrom in order, then substitute,
// newlines removed as the controller does), nil without postBuild, with a masker for the
// values read from Secrets.
func fluxPostBuildVars(ctx context.Context, k *kubeClient, spec fluxKustomizeSpec, namespace string) (map[string]string, *kubeDiffMasker, error) {
	masker := newKubeDiffMasker()
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

		err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/"+plural+"/"+url.PathEscape(from.Name), &obj)
		if err != nil {
			if isNotFound(err) && from.Optional {
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

				masker.add(string(d))
				v = strings.ReplaceAll(string(d), "\n", "")
				masker.add(v)
			}

			vars[key] = strings.ReplaceAll(v, "\n", "")
		}
	}

	for key, v := range spec.PostBuild.Substitute {
		vars[key] = strings.ReplaceAll(v, "\n", "")
	}

	for key := range vars {
		if !fluxVarName.MatchString(key) {
			return nil, nil, fmt.Errorf("postBuild: variable name %q is invalid, it must match %s", key, fluxVarName)
		}
	}

	return vars, masker, nil
}

// diffFluxBuilt compares each built object with the cluster, fluxDiffParallel at a time.
func diffFluxBuilt(ctx context.Context, k *kubeClient, index *kubeResourceIndex, built []fluxBuilt, masker *kubeDiffMasker) []kubeDiffResource {
	out := make([]kubeDiffResource, len(built))
	sem := make(chan struct{}, fluxDiffParallel)

	var wg sync.WaitGroup

	for i, b := range built {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			out[i] = diffFluxObject(ctx, k, index, b, masker)
		})
	}

	wg.Wait()

	return out
}

func diffFluxObject(ctx context.Context, k *kubeClient, index *kubeResourceIndex, b fluxBuilt, masker *kubeDiffMasker) kubeDiffResource {
	apiVersion, _ := b.obj["apiVersion"].(string)
	kind, _ := b.obj["kind"].(string)
	meta, _ := b.obj["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	namespace, _ := meta["namespace"].(string)

	group, version := splitAPIVersion(apiVersion)
	r := kubeDiffResource{Group: group, Version: version, Kind: kind, Namespace: namespace, Name: name}

	if b.encrypted {
		r.Change = diffChangeEncrypted

		return r
	}

	fail := func(err error) kubeDiffResource {
		r.Change, r.Error = diffChangeError, masker.mask(err.Error())

		return r
	}

	res, err := index.resource(ctx, apiVersion, kind)
	if err != nil {
		return fail(err)
	}

	if res.Namespaced && namespace == "" {
		return fail(errors.New("namespaced object without a namespace"))
	}

	if !res.Namespaced {
		r.Namespace = ""
	}

	path, err := index.path(ctx, apiVersion, kind, namespace, name)
	if err != nil {
		return fail(err)
	}

	if fluxExcluded(b.obj) {
		r.Change = diffChangeIgnored

		return r
	}

	live, err := readLive(ctx, k, path)
	if err != nil {
		return fail(err)
	}

	if live != nil && (fluxExcluded(live) || strings.EqualFold(annotation(b.obj, fluxSSAAnnotation), "IfNotPresent")) {
		r.Change = diffChangeIgnored

		return r
	}

	wanted, err := dryRunApply(ctx, k, path, fluxFieldManager, b.obj)
	if err != nil {
		return fail(err)
	}

	diffObjects(&r, live, wanted, masker)

	return r
}

// diffFluxPruned is the inventory entry id that the build no longer has: deleted when it
// still exists and may be pruned.
func diffFluxPruned(ctx context.Context, k *kubeClient, index *kubeResourceIndex, id, version string, masker *kubeDiffMasker) (kubeDiffResource, bool) {
	e, ok := parseFluxInventoryID(id)
	if !ok || version == "" {
		return kubeDiffResource{}, false
	}

	apiVersion := version
	if e.Group != "" {
		apiVersion = e.Group + "/" + version
	}

	r := kubeDiffResource{Group: e.Group, Version: version, Kind: e.Kind, Namespace: e.Namespace, Name: e.Name}

	path, err := index.path(ctx, apiVersion, e.Kind, e.Namespace, e.Name)
	if err != nil {
		return kubeDiffResource{}, false
	}

	live, err := readLive(ctx, k, path)
	if err != nil {
		r.Change, r.Error = diffChangeError, masker.mask(err.Error())

		return r, true
	}

	if live == nil {
		return kubeDiffResource{}, false
	}

	if strings.EqualFold(metaValue(live, fluxPruneAnnotation), "disabled") || fluxExcluded(live) {
		r.Change = diffChangeIgnored

		return r, true
	}

	diffObjects(&r, live, nil, masker)

	return r, true
}

// fluxExcluded is an object kustomize-controller never applies: reconcile disabled, or ssa
// Ignore (label or annotation).
func fluxExcluded(obj map[string]any) bool {
	return strings.EqualFold(metaValue(obj, fluxReconcileAnnotation), "disabled") || strings.EqualFold(metaValue(obj, fluxSSAAnnotation), "Ignore")
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

func splitAPIVersion(apiVersion string) (group, version string) {
	if g, v, ok := strings.Cut(apiVersion, "/"); ok {
		return g, v
	}

	return "", apiVersion
}

// diffChangeOrder lists what needs a look first.
var diffChangeOrder = []string{diffChangeError, diffChangeCreated, diffChangeChanged, diffChangeDeleted, diffChangeEncrypted, diffChangeIgnored, diffChangeUnchanged}

func sortDiffResources(rs []kubeDiffResource) {
	slices.SortStableFunc(rs, func(a, b kubeDiffResource) int {
		if c := cmp.Compare(slices.Index(diffChangeOrder, a.Change), slices.Index(diffChangeOrder, b.Change)); c != 0 {
			return c
		}

		switch {
		case a.less(b):
			return -1
		case b.less(a):
			return 1
		default:
			return 0
		}
	})
}
