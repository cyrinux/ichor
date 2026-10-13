package ichorgo

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Only the metadata, Crossplane references and conditions are decoded. Provider-specific
// desired state, status payloads and connection secrets are neither returned nor followed.
type xpCompositeSpec struct {
	CompositionRef xpNameRef   `json:"compositionRef"`
	ResourceRefs   []xpNameRef `json:"resourceRefs"`
}

type xpResourceObject struct {
	Metadata xpMetadata `json:"metadata"`
	Spec     struct {
		xpCompositeSpec
		Crossplane         *xpCompositeSpec `json:"crossplane"`
		ClaimRef           xpNameRef        `json:"claimRef"`
		ResourceRef        xpNameRef        `json:"resourceRef"`
		ProviderConfigRef  xpNameRef        `json:"providerConfigRef"`
		ManagementPolicies []string         `json:"managementPolicies"`
	} `json:"spec"`
	Status struct {
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

func mapCrossplaneObject(o xpResourceObject, ref xpObjectRef, now time.Time) xpObject {
	x := xpObject{xpObjectRef: ref, Claim: o.Spec.ClaimRef.Name, Composite: o.Spec.ResourceRef.Name, ProviderConfig: o.Spec.ProviderConfigRef.Name, ExternalName: o.Metadata.Annotations["crossplane.io/external-name"], ManagementPolicies: nonNil(o.Spec.ManagementPolicies), Ready: xpConditionOf(o.Status.Conditions, "Ready"), Synced: xpConditionOf(o.Status.Conditions, "Synced"), Health: healthOK, Reasons: []string{}}
	x.Name = o.Metadata.Name
	x.Namespace = o.Metadata.Namespace
	compositeSpec := o.Spec.xpCompositeSpec
	if o.Spec.Crossplane != nil {
		compositeSpec = *o.Spec.Crossplane
	}
	x.Composition = compositeSpec.CompositionRef.Name
	x.Resources = len(compositeSpec.ResourceRefs)
	if label := o.Metadata.Labels["crossplane.io/composite"]; label != "" {
		x.Composite = label
	}
	x.Paused = xpPaused(o.Metadata.Annotations, x.Synced)
	if x.Synced.Status == "False" && x.Synced.Reason == "ReconcileError" {
		x.Health = healthCritical
		x.Reasons = append(x.Reasons, xpReasonSyncFailed)
		x.LastTransition = x.Synced.LastTransition
	}
	if x.Ready.Status == "False" && (x.Ready.Reason == "Unavailable" || (x.Ready.LastTransition > 0 && now.Sub(time.UnixMilli(x.Ready.LastTransition)) > xpNotReadyGrace && x.Ready.Reason != "Deleting")) {
		x.Reasons = append(x.Reasons, xpReasonNotReady)
		if x.LastTransition == 0 {
			x.LastTransition = x.Ready.LastTransition
		}
	}
	if o.Metadata.DeletionTimestamp != "" || x.Ready.Reason == "Deleting" {
		x.Reasons = append(x.Reasons, xpReasonDeleting)
		if x.LastTransition == 0 {
			x.LastTransition = x.Ready.LastTransition
		}
	}
	if x.Paused {
		x.Reasons = append(x.Reasons, xpReasonPaused)
		if x.LastTransition == 0 {
			x.LastTransition = x.Synced.LastTransition
		}
	}
	if x.Health != healthCritical && len(x.Reasons) > 0 {
		x.Health = healthWarning
	}
	return x
}

// xpManagedRows holds all problem rows and a bounded, deterministic sample of healthy
// rows. Counts cover every decoded row, including the healthy rows omitted from the UI.
type xpManagedRows struct {
	problems []xpObject
	healthy  []xpObject
	total    int
	counts   xpCounts
}

func (a *xpManagedRows) add(items []xpObject) {
	for _, x := range items {
		a.total++
		a.counts.add(x.Health)
		if x.Health == healthOK {
			a.healthy = append(a.healthy, x)
		} else {
			a.problems = append(a.problems, x)
		}
	}
	a.capHealthy()
}

func (a *xpManagedRows) capHealthy() {
	slices.SortFunc(a.healthy, func(a, b xpObject) int { return strings.Compare(a.key(), b.key()) })
	if len(a.healthy) > xpManagedOKLimit {
		a.healthy = a.healthy[:xpManagedOKLimit]
	}
}

func (a *xpManagedRows) merge(b xpManagedRows) {
	a.total += b.total
	a.counts.merge(b.counts)
	a.problems = append(a.problems, b.problems...)
	a.healthy = append(a.healthy, b.healthy...)
	a.capHealthy()
}

func listCrossplaneManaged(ctx context.Context, k *kubeClient, resources []kubeBrowserResource, now time.Time) ([]xpObject, int, xpCounts, error) {
	// At most eight kinds are held locally at once. A list restart resets that kind's counts,
	// avoiding double-counting after an expired continuation token.
	var total xpManagedRows
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make([]error, len(resources))
	jobs := make(chan int)
	for range min(discoveryParallel, len(resources)) {
		wg.Go(func() {
			for i := range jobs {
				r := resources[i]
				ref := xpReference(r.Group, r.Version, r.Resource, r.Kind)
				var local xpManagedRows
				err := k.listAll(ctx, ref.path(), pageQuery{limit: 500}, func() { local = xpManagedRows{} }, func(page kubePage) error {
					objects, err := decodeItems[xpResourceObject](page)
					if err != nil {
						return err
					}
					rows := make([]xpObject, 0, len(objects))
					for _, o := range objects {
						rows = append(rows, mapCrossplaneObject(o, ref, now))
					}
					local.add(rows)
					return nil
				})
				if err != nil {
					errs[i] = fmt.Errorf("%s: %v", ref.path(), kubeError(err))
				}
				mu.Lock()
				total.merge(local)
				mu.Unlock()
			}
		})
	}
	for i := range resources {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	rows := append([]xpObject{}, total.problems...)
	rows = append(rows, total.healthy...)
	sortCrossplaneObjects(rows)
	return rows, total.total, total.counts, crossplaneErrors(errs)
}
