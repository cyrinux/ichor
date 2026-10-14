package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

type xpMetadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Annotations       map[string]string `json:"annotations"`
	Labels            map[string]string `json:"labels"`
	DeletionTimestamp string            `json:"deletionTimestamp"`
}

type xpNameRef struct {
	Name string `json:"name"`
}
type xpTypeNames struct {
	Kind   string `json:"kind"`
	Plural string `json:"plural"`
}

type xpPackageObject struct {
	Metadata xpMetadata `json:"metadata"`
	Spec     struct {
		Package string `json:"package"`
	} `json:"spec"`
	Status struct {
		CurrentRevision   string         `json:"currentRevision"`
		CurrentIdentifier string         `json:"currentIdentifier"`
		Conditions        kubeConditions `json:"conditions"`
	} `json:"status"`
}

type xpDefinitionObject struct {
	Metadata xpMetadata `json:"metadata"`
	Spec     struct {
		Group      string                `json:"group"`
		Names      xpTypeNames           `json:"names"`
		Scope      string                `json:"scope"`
		ClaimNames xpTypeNames           `json:"claimNames"`
		Versions   []xpDefinitionVersion `json:"versions"`
	} `json:"spec"`
	Status struct {
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

type xpCompositionObject struct {
	Metadata xpMetadata `json:"metadata"`
	Spec     struct {
		CompositeTypeRef struct {
			Kind       string `json:"kind"`
			APIVersion string `json:"apiVersion"`
		} `json:"compositeTypeRef"`
	} `json:"spec"`
}

func readCrossplanePackages(ctx context.Context, k *kubeClient) ([]xpPackage, error) {
	resources := []string{"providers", "configurations", "functions"}
	kinds := []string{"Provider", "Configuration", "Function"}
	rows := make([][]xpPackage, len(resources))
	errs := make([]error, len(resources))
	var wg sync.WaitGroup
	for i, resource := range resources {
		wg.Go(func() {
			ref := xpReference(groupCrossplanePkg, xpVersion, resource, kinds[i])
			objects, err := xpList[xpPackageObject](ctx, k, ref.path())
			errs[i] = err
			for _, o := range objects {
				p := xpPackage{xpObjectRef: ref, Package: o.Spec.Package, CurrentRevision: o.Status.CurrentRevision, CurrentIdentifier: o.Status.CurrentIdentifier, Installed: xpConditionOf(o.Status.Conditions, "Installed"), Healthy: xpConditionOf(o.Status.Conditions, "Healthy"), Health: healthOK, Reasons: []string{}}
				p.Name = o.Metadata.Name
				p.Namespace = o.Metadata.Namespace
				switch {
				case p.Installed.Status == "False" || p.Healthy.Status == "False":
					p.Health = healthCritical
					p.Reasons = append(p.Reasons, xpReasonProviderUnhealthy)
				case p.Installed.Status != "True" || p.Healthy.Status != "True":
					p.Health = healthWarning
					p.Reasons = append(p.Reasons, xpReasonProviderUnhealthy)
				}
				rows[i] = append(rows[i], p)
			}
		})
	}
	wg.Wait()
	out := []xpPackage{}
	for _, rs := range rows {
		out = append(out, rs...)
	}
	slices.SortFunc(out, byHealthThenKey(func(x xpPackage) (string, string) { return x.Health, x.key() }))
	return out, errors.Join(errs...)
}

func readCrossplaneDefinitions(ctx context.Context, k *kubeClient, version string) ([]xpDefinition, error) {
	if version == "" {
		version = xpVersion
	}
	ref := xpReference(groupCrossplaneAPIExt, version, "compositeresourcedefinitions", "CompositeResourceDefinition")
	var definitions []xpDefinitionObject
	var compositions []xpCompositionObject
	var definitionErr, compositionErr error
	var wg sync.WaitGroup
	wg.Go(func() { definitions, definitionErr = xpList[xpDefinitionObject](ctx, k, ref.path()) })
	wg.Go(func() {
		compositions, compositionErr = xpList[xpCompositionObject](ctx, k, "/apis/"+groupCrossplaneAPIExt+"/"+xpVersion+"/compositions")
	})
	wg.Wait()
	out := []xpDefinition{}
	for _, o := range definitions {
		d := xpDefinition{xpObjectRef: ref, CompositeGroup: o.Spec.Group, CompositeKind: o.Spec.Names.Kind, Plural: o.Spec.Names.Plural, Scope: o.Spec.Scope, Versions: nonNil(o.Spec.Versions), Established: xpConditionOf(o.Status.Conditions, "Established"), Offered: xpConditionOf(o.Status.Conditions, "Offered"), Health: healthOK, Reasons: []string{}, CompositionNames: []string{}}
		d.Name = o.Metadata.Name
		if d.Scope == "" {
			d.Scope = "LegacyCluster"
		}
		if d.Scope == "LegacyCluster" {
			d.ClaimKind = o.Spec.ClaimNames.Kind
			d.ClaimPlural = o.Spec.ClaimNames.Plural
		}
		if d.Established.Status != "True" {
			d.Health = healthWarning
			d.Reasons = append(d.Reasons, xpReasonNotEstablished)
		}
		for _, c := range compositions {
			if c.Spec.CompositeTypeRef.Kind != d.CompositeKind {
				continue
			}
			for _, v := range d.Versions {
				if v.Served && c.Spec.CompositeTypeRef.APIVersion == d.CompositeGroup+"/"+v.Name {
					d.Compositions++
					d.CompositionNames = append(d.CompositionNames, c.Metadata.Name)
					break
				}
			}
		}
		slices.Sort(d.CompositionNames)
		if len(d.CompositionNames) > 20 {
			d.CompositionNames = d.CompositionNames[:20]
		}
		out = append(out, d)
	}
	slices.SortFunc(out, byHealthThenKey(func(x xpDefinition) (string, string) { return x.Health, x.key() }))
	return out, errors.Join(definitionErr, compositionErr)
}

func readCrossplaneComposites(ctx context.Context, k *kubeClient, definitions []xpDefinition, now time.Time) ([]xpObject, []xpObject, error) {
	type collection struct {
		ref   xpObjectRef
		claim bool
	}
	collections := []collection{}
	errs := []error{}
	for _, d := range definitions {
		if d.Established.Status != "True" {
			continue
		}
		version := ""
		for _, v := range d.Versions {
			if v.Served && v.Referenceable {
				version = v.Name
				break
			}
		}
		if version == "" {
			errs = append(errs, fmt.Errorf("XRD %s: no served referenceable version", d.Name))
			continue
		}
		ref, err := newResourceRef(d.CompositeGroup, version, d.Plural)
		if err != nil || d.CompositeGroup == "" {
			errs = append(errs, fmt.Errorf("XRD %s: invalid composite resource reference", d.Name))
			continue
		}
		collections = append(collections, collection{xpReference(ref.group, ref.version, ref.resource, d.CompositeKind), false})
		if d.Scope == "LegacyCluster" && d.ClaimPlural != "" {
			claimRef, err := newResourceRef(d.CompositeGroup, version, d.ClaimPlural)
			if err != nil {
				errs = append(errs, fmt.Errorf("XRD %s: %w", d.Name, err))
				continue
			}
			collections = append(collections, collection{xpReference(claimRef.group, claimRef.version, claimRef.resource, d.ClaimKind), true})
		}
	}
	rows := make([][]xpObject, len(collections))
	listErrs := make([]error, len(collections))
	slots := make(chan struct{}, discoveryParallel)
	var wg sync.WaitGroup
	for i, c := range collections {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			objects, err := xpList[xpResourceObject](ctx, k, c.ref.path())
			listErrs[i] = err
			for _, o := range objects {
				rows[i] = append(rows[i], mapCrossplaneObject(o, c.ref, now))
			}
		})
	}
	wg.Wait()
	composites, claims := []xpObject{}, []xpObject{}
	for i, rs := range rows {
		if collections[i].claim {
			claims = append(claims, rs...)
		} else {
			composites = append(composites, rs...)
		}
	}
	sortCrossplaneObjects(composites)
	sortCrossplaneObjects(claims)
	return composites, claims, errors.Join(append(errs, listErrs...)...)
}
