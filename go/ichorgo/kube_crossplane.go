package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Crossplane packages describe the control plane; XRDs and discovery describe its dynamic
// resource kinds. Nothing in this reader depends on a particular cloud provider.
const (
	groupCrossplanePkg        = "pkg.crossplane.io"
	groupCrossplaneAPIExt     = "apiextensions.crossplane.io"
	xpVersion                 = "v1"
	xpManagedOKLimit          = 500
	xpNotReadyGrace           = 15 * time.Minute
	xpReasonSyncFailed        = "syncFailed"
	xpReasonProviderUnhealthy = "providerUnhealthy"
	xpReasonNotReady          = "notReady"
	xpReasonDeleting          = "deleting"
	xpReasonPaused            = "paused"
	xpReasonNotEstablished    = "notEstablished"
)

type crossplaneStatus struct {
	Installed    bool           `json:"installed"`
	Version      string         `json:"version"`
	Error        string         `json:"error"`
	Packages     []xpPackage    `json:"packages"`
	Definitions  []xpDefinition `json:"definitions"`
	Composites   []xpObject     `json:"composites"`
	Claims       []xpObject     `json:"claims"`
	Managed      []xpObject     `json:"managed"`
	ManagedTotal int            `json:"managedTotal"`
	// Only healthy managed rows are capped; every problem and all counts survive truncation.
	Truncated bool     `json:"truncated"`
	Counts    xpCounts `json:"counts"`
}

type xpCounts struct {
	OK       int `json:"ok"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
}

func (c *xpCounts) add(health string) {
	switch health {
	case healthCritical:
		c.Critical++
	case healthWarning:
		c.Warning++
	default:
		c.OK++
	}
}

func (c *xpCounts) merge(other xpCounts) {
	c.OK += other.OK
	c.Warning += other.Warning
	c.Critical += other.Critical
}

type xpObjectRef struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func (r xpObjectRef) path() string { return (resourceRef{r.Group, r.Version, r.Resource}).path("") }
func (r xpObjectRef) key() string {
	return r.Group + "/" + r.Resource + "/" + r.Namespace + "/" + r.Name
}

type xpCondition struct {
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	LastTransition int64  `json:"lastTransition"`
}

func xpConditionOf(conditions kubeConditions, typ string) xpCondition {
	c := conditions.get(typ)
	return xpCondition{c.Status, c.Reason, c.Message, unixMilli(c.LastTransitionTime)}
}

type xpPackage struct {
	xpObjectRef
	Package           string      `json:"package"`
	CurrentRevision   string      `json:"currentRevision"`
	CurrentIdentifier string      `json:"currentIdentifier"`
	Installed         xpCondition `json:"installed"`
	Healthy           xpCondition `json:"healthy"`
	Health            string      `json:"health"`
	Reasons           []string    `json:"reasons"`
}

type xpDefinition struct {
	xpObjectRef
	CompositeGroup   string                `json:"compositeGroup"`
	CompositeKind    string                `json:"compositeKind"`
	Plural           string                `json:"plural"`
	Scope            string                `json:"scope"`
	ClaimKind        string                `json:"claimKind"`
	ClaimPlural      string                `json:"claimPlural"`
	Versions         []xpDefinitionVersion `json:"versions"`
	Established      xpCondition           `json:"established"`
	Offered          xpCondition           `json:"offered"`
	Compositions     int                   `json:"compositions"`
	CompositionNames []string              `json:"compositionNames"`
	Composites       int                   `json:"composites"`
	Health           string                `json:"health"`
	Reasons          []string              `json:"reasons"`
}

type xpDefinitionVersion struct {
	Name          string `json:"name"`
	Served        bool   `json:"served"`
	Referenceable bool   `json:"referenceable"`
}

type xpObject struct {
	xpObjectRef
	Composition        string      `json:"composition"`
	Resources          int         `json:"resources"`
	Claim              string      `json:"claim"`
	Composite          string      `json:"composite"`
	ProviderConfig     string      `json:"providerConfig"`
	ExternalName       string      `json:"externalName"`
	Paused             bool        `json:"paused"`
	ManagementPolicies []string    `json:"managementPolicies"`
	Ready              xpCondition `json:"ready"`
	Synced             xpCondition `json:"synced"`
	LastTransition     int64       `json:"lastTransition"`
	Health             string      `json:"health"`
	Reasons            []string    `json:"reasons"`
}

// KubeCrossplane reads packages, definitions, composite resources, claims and managed
// resources with their conditions. List permissions on those resources are sufficient;
// forbidden lists appear in error while readable sections still return. No secrets are read.
// kubeServer: see KubePods.
func KubeCrossplane(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() crossplaneStatus { return demoCrossplane(time.Now()) }, func(ctx context.Context, k *kubeClient) (crossplaneStatus, error) {
		groups, err := readAPIGroups(ctx, k)
		if err != nil {
			return crossplaneStatus{}, err
		}
		return readCrossplane(ctx, k, groups, time.Now())
	})
}

func emptyCrossplane() crossplaneStatus {
	return crossplaneStatus{Packages: []xpPackage{}, Definitions: []xpDefinition{}, Composites: []xpObject{}, Claims: []xpObject{}, Managed: []xpObject{}}
}

func readCrossplane(ctx context.Context, k *kubeClient, groups map[string]string, now time.Time) (crossplaneStatus, error) {
	out := emptyCrossplane()
	if _, pkg := groups[groupCrossplanePkg]; !pkg {
		if _, ext := groups[groupCrossplaneAPIExt]; !ext {
			return out, nil
		}
	}
	out.Installed = true
	out.Version = xpVersion // The stable package/XRD API, not the installed controller image version.
	var resources []kubeBrowserResource
	var packageErr, definitionErr, discoveryErr error
	var wg sync.WaitGroup
	if _, ok := groups[groupCrossplanePkg]; ok {
		wg.Go(func() { out.Packages, packageErr = readCrossplanePackages(ctx, k) })
	}
	if _, ok := groups[groupCrossplaneAPIExt]; ok {
		wg.Go(func() { out.Definitions, definitionErr = readCrossplaneDefinitions(ctx, k) })
	}
	wg.Go(func() { resources, discoveryErr = discoverCrossplaneManaged(ctx, k, groups) })
	wg.Wait()
	var compositeErr, managedErr error
	wg.Go(func() {
		out.Composites, out.Claims, compositeErr = readCrossplaneComposites(ctx, k, out.Definitions, now)
	})
	wg.Go(func() {
		out.Managed, out.ManagedTotal, out.Counts, managedErr = listCrossplaneManaged(ctx, k, resources, now)
	})
	wg.Wait()
	out.Truncated = out.ManagedTotal > len(out.Managed)
	for _, p := range out.Packages {
		out.Counts.add(p.Health)
	}
	for i := range out.Definitions {
		d := &out.Definitions[i]
		for _, x := range out.Composites {
			if x.Group == d.CompositeGroup && x.Kind == d.CompositeKind {
				d.Composites++
			}
		}
		out.Counts.add(d.Health)
	}
	for _, x := range out.Composites {
		out.Counts.add(x.Health)
	}
	for _, x := range out.Claims {
		out.Counts.add(x.Health)
	}
	out.Error = sectionError(packageErr, definitionErr, discoveryErr, compositeErr, managedErr)
	return out, nil
}

// discoverCrossplaneManaged reuses the browser's discovery decoding/filtering without
// requiring core discovery or listing CRDs. A failed group never discards other groups.
func discoverCrossplaneManaged(ctx context.Context, k *kubeClient, groups map[string]string) ([]kubeBrowserResource, error) {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	slices.Sort(names)
	results := make([][]kubeBrowserResource, len(names))
	errs := make([]error, len(names))
	slots := make(chan struct{}, discoveryParallel)
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			path := "/apis/" + name + "/" + groups[name]
			var doc discoveryResourceList
			if err := k.get(ctx, path, &doc); err != nil {
				errs[i] = fmt.Errorf("%s: %v", path, kubeError(err))
				return
			}
			// Some discovery servers omit groupVersion; the requested path is authoritative.
			doc.GroupVersion = name + "/" + groups[name]
			for _, r := range listableResources(doc) {
				if slices.Contains(r.Categories, "managed") {
					results[i] = append(results[i], r)
				}
			}
		})
	}
	wg.Wait()
	out := []kubeBrowserResource{}
	for _, rs := range results {
		out = append(out, rs...)
	}
	return out, crossplaneErrors(errs)
}

// xpList reads paged typed objects; errors retain completed pages for a partial report.
func xpList[T any](ctx context.Context, k *kubeClient, path string) ([]T, error) {
	out := []T{}
	err := k.listAll(ctx, path, pageQuery{limit: 500}, func() { out = out[:0] }, func(page kubePage) error {
		items, err := decodeItems[T](page)
		if err != nil {
			return err
		}
		out = append(out, items...)
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("%s: %v", path, kubeError(err))
	}
	return out, nil
}

func sortCrossplaneObjects(items []xpObject) {
	slices.SortFunc(items, byHealthThenKey(func(x xpObject) (string, string) { return x.Health, x.key() }))
}

// Keep provider error messages in the JSON so the caller can diagnose them; never log them.
func crossplaneErrors(errs []error) error {
	return errors.Join(errs...)
}

func xpPaused(annotations map[string]string, c xpCondition) bool {
	return annotations["crossplane.io/paused"] == "true" || c.Reason == "ReconcilePaused"
}

func xpReference(group, version, resource, kind string) xpObjectRef {
	return xpObjectRef{Group: group, Version: version, Resource: resource, Kind: kind}
}
