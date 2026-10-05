package ichorgo

import (
	"cmp"
	"strings"
)

// The fields of the Flux v2 objects the app reads (kustomize v1, helm v2 and v2beta*,
// source v1 and v1beta2 all share them).

type fluxMeta struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ResourceVersion string            `json:"resourceVersion"`
	Labels          map[string]string `json:"labels"`
	Annotations     map[string]string `json:"annotations"`
}

type fluxConditionObject struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

type fluxConditions []fluxConditionObject

func (c fluxConditions) get(t string) fluxConditionObject {
	for _, x := range c {
		if x.Type == t {
			return x
		}
	}

	return fluxConditionObject{Status: "Unknown"}
}

func (c fluxConditions) is(t string) bool { return c.get(t).Status == "True" }

type fluxCrossRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type fluxCommonStatus struct {
	Conditions             fluxConditions `json:"conditions"`
	LastHandledReconcileAt string         `json:"lastHandledReconcileAt"`
	LastAppliedRevision    string         `json:"lastAppliedRevision"`
	LastAttemptedRevision  string         `json:"lastAttemptedRevision"`
}

type fluxKustomizationObject struct {
	Metadata fluxMeta `json:"metadata"`
	Spec     struct {
		SourceRef       fluxCrossRef   `json:"sourceRef"`
		Path            string         `json:"path"`
		Interval        string         `json:"interval"`
		Suspend         bool           `json:"suspend"`
		Prune           bool           `json:"prune"`
		TargetNamespace string         `json:"targetNamespace"`
		DependsOn       []fluxCrossRef `json:"dependsOn"`
	} `json:"spec"`
	Status struct {
		fluxCommonStatus
		Inventory *struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"inventory"`
	} `json:"status"`
}

type fluxHelmReleaseObject struct {
	Metadata fluxMeta `json:"metadata"`
	Spec     struct {
		Chart *struct {
			Spec struct {
				Chart     string       `json:"chart"`
				Version   string       `json:"version"`
				SourceRef fluxCrossRef `json:"sourceRef"`
			} `json:"spec"`
		} `json:"chart"`
		ChartRef         *fluxCrossRef  `json:"chartRef"`
		Interval         string         `json:"interval"`
		Suspend          bool           `json:"suspend"`
		TargetNamespace  string         `json:"targetNamespace"`
		StorageNamespace string         `json:"storageNamespace"`
		DependsOn        []fluxCrossRef `json:"dependsOn"`
	} `json:"spec"`
	Status struct {
		fluxCommonStatus
		History []struct {
			Version       int    `json:"version"`
			ChartName     string `json:"chartName"`
			ChartVersion  string `json:"chartVersion"`
			AppVersion    string `json:"appVersion"`
			Status        string `json:"status"`
			FirstDeployed string `json:"firstDeployed"`
			LastDeployed  string `json:"lastDeployed"`
		} `json:"history"`
		InstallFailures int `json:"installFailures"`
		UpgradeFailures int `json:"upgradeFailures"`
	} `json:"status"`
}

type fluxSourceObject struct {
	Kind     string   `json:"kind"`
	Metadata fluxMeta `json:"metadata"`
	Spec     struct {
		URL        string `json:"url"`
		Type       string `json:"type"` // a HelmRepository's "oci"
		Interval   string `json:"interval"`
		Suspend    bool   `json:"suspend"`
		BucketName string `json:"bucketName"`
		Endpoint   string `json:"endpoint"`
		Ref        *struct {
			Branch string `json:"branch"`
			Tag    string `json:"tag"`
			SemVer string `json:"semver"`
			Name   string `json:"name"`
			Commit string `json:"commit"`
			Digest string `json:"digest"`
		} `json:"ref"`
	} `json:"spec"`
	Status struct {
		fluxCommonStatus
		Artifact *struct {
			Revision       string `json:"revision"`
			LastUpdateTime string `json:"lastUpdateTime"`
		} `json:"artifact"`
	} `json:"status"`
}

// fluxApplyCommon fills what Kustomizations and HelmReleases share.
func fluxApplyCommon(a *fluxApp, m fluxMeta, st fluxCommonStatus, suspended bool, deps []fluxCrossRef) {
	ready := st.Conditions.get("Ready")

	a.Namespace, a.Name = m.Namespace, m.Name
	a.Ready, a.Reason, a.Message = ready.Status, ready.Reason, ready.Message
	a.Reconciling, a.Stalled, a.Suspended = st.Conditions.is("Reconciling"), st.Conditions.is("Stalled"), suspended
	a.Pending = fluxPending(m, st)
	a.Revision, a.AttemptedRevision = st.LastAppliedRevision, st.LastAttemptedRevision
	a.ReconciledAt = unixMilli(ready.LastTransitionTime)
	a.Level = fluxLevel(suspended, st.Conditions)
	a.Conditions = fluxConditionsOf(st.Conditions)
	a.DependsOn = []string{}
	a.Resources = []fluxResource{}
	a.History = []fluxHistory{}
	a.UnhealthyPods = []kubePod{}

	for _, d := range deps {
		a.DependsOn = append(a.DependsOn, cmp.Or(d.Namespace, m.Namespace)+"/"+d.Name)
	}

	if name := m.Labels[fluxOwnerName]; name != "" {
		a.Owner = &fluxRef{Kind: "Kustomization", Namespace: m.Labels[fluxOwnerNamespace], Name: name}
	}
}

// fluxPending: a reconcile was requested and the controller has not handled it yet.
func fluxPending(m fluxMeta, st fluxCommonStatus) bool {
	req := m.Annotations[fluxRequestedAt]

	return req != "" && req != st.LastHandledReconcileAt
}

func fluxConditionsOf(list fluxConditions) []fluxCondition {
	out := make([]fluxCondition, 0, len(list))
	for _, c := range list {
		out = append(out, fluxCondition{Type: c.Type, Status: c.Status, Reason: c.Reason, Message: c.Message, At: unixMilli(c.LastTransitionTime)})
	}

	return out
}

func fluxSourceRef(ref fluxCrossRef, namespace string, urls map[fluxRef]string) (*fluxRef, string) {
	if ref.Name == "" {
		return nil, ""
	}

	r := fluxRef{Kind: ref.Kind, Namespace: cmp.Or(ref.Namespace, namespace), Name: ref.Name}

	return &r, urls[r]
}

func mapFluxKustomization(o fluxKustomizationObject, urls map[fluxRef]string) fluxApp {
	a := fluxApp{Kind: "Kustomization", Path: o.Spec.Path, Interval: o.Spec.Interval, Prune: o.Spec.Prune, TargetNamespace: o.Spec.TargetNamespace}
	fluxApplyCommon(&a, o.Metadata, o.Status.fluxCommonStatus, o.Spec.Suspend, o.Spec.DependsOn)
	a.Source, a.SourceURL = fluxSourceRef(o.Spec.SourceRef, o.Metadata.Namespace, urls)

	namespaces := map[string]bool{}

	if inv := o.Status.Inventory; inv != nil {
		for _, e := range inv.Entries {
			r, ok := parseFluxInventoryID(e.ID)
			if !ok {
				continue
			}

			if r.Namespace != "" && !namespaces[r.Namespace] {
				namespaces[r.Namespace] = true
				a.namespaces = append(a.namespaces, r.Namespace)
			}

			if len(a.Resources) < fluxResourcesMax {
				a.Resources = append(a.Resources, r)
			}
		}
	}

	if a.TargetNamespace != "" {
		a.namespaces = []string{a.TargetNamespace}
	}

	a.Icon, a.RemoteIcon = fluxIcon(a.Name)

	return a
}

// parseFluxInventoryID reads "<namespace>_<name>_<group>_<kind>" (namespace and group may be
// empty; Kubernetes names hold no "_").
func parseFluxInventoryID(id string) (fluxResource, bool) {
	parts := strings.Split(id, "_")
	if len(parts) != 4 || parts[1] == "" || parts[3] == "" {
		return fluxResource{}, false
	}

	return fluxResource{Namespace: parts[0], Name: parts[1], Group: parts[2], Kind: parts[3]}, true
}

func mapFluxHelmRelease(o fluxHelmReleaseObject, urls map[fluxRef]string) fluxApp {
	a := fluxApp{Kind: "HelmRelease", Interval: o.Spec.Interval, TargetNamespace: o.Spec.TargetNamespace}
	fluxApplyCommon(&a, o.Metadata, o.Status.fluxCommonStatus, o.Spec.Suspend, o.Spec.DependsOn)
	a.Failures = o.Status.InstallFailures + o.Status.UpgradeFailures
	a.namespaces = []string{cmp.Or(o.Spec.TargetNamespace, o.Metadata.Namespace)}

	switch {
	case o.Spec.ChartRef != nil:
		a.Source, a.SourceURL = fluxSourceRef(*o.Spec.ChartRef, o.Metadata.Namespace, urls)
	case o.Spec.Chart != nil:
		c := o.Spec.Chart.Spec
		a.Chart, a.ChartVersion = c.Chart, c.Version
		a.Source, a.SourceURL = fluxSourceRef(c.SourceRef, o.Metadata.Namespace, urls)
	}

	h := o.Status.History
	for i := 0; i < len(h) && len(a.History) < fluxHistoryMax; i++ {
		a.History = append(a.History, fluxHistory{
			Version: h[i].Version, ChartVersion: h[i].ChartVersion, AppVersion: h[i].AppVersion, Status: h[i].Status,
			DeployedAt: unixMilli(cmp.Or(h[i].LastDeployed, h[i].FirstDeployed)),
		})
	}

	if len(h) > 0 {
		// helm v2 keeps the release history instead of lastAppliedRevision.
		a.Revision = cmp.Or(a.Revision, h[0].ChartVersion)
		a.Chart = cmp.Or(a.Chart, h[0].ChartName)
	}

	a.Icon, a.RemoteIcon = fluxIcon(a.Chart, a.Name)

	return a
}

func mapFluxSource(o fluxSourceObject) fluxSource {
	st := o.Status
	ready := st.Conditions.get("Ready")

	s := fluxSource{
		Kind: o.Kind, Namespace: o.Metadata.Namespace, Name: o.Metadata.Name,
		URL: o.Spec.URL, Interval: o.Spec.Interval, Suspended: o.Spec.Suspend,
		Ready: ready.Status, Reason: ready.Reason, Message: ready.Message,
		Reconciling: st.Conditions.is("Reconciling"), Pending: fluxPending(o.Metadata, st.fluxCommonStatus),
		Level: fluxLevel(o.Spec.Suspend, st.Conditions),
	}

	if s.URL == "" && o.Spec.BucketName != "" {
		s.URL = strings.TrimSuffix(o.Spec.Endpoint, "/") + "/" + o.Spec.BucketName
	}

	if r := o.Spec.Ref; r != nil {
		s.Ref = cmp.Or(r.Name, r.Commit, r.Digest, r.SemVer, r.Tag, r.Branch)
	}

	if a := st.Artifact; a != nil {
		s.Revision, s.FetchedAt = a.Revision, unixMilli(a.LastUpdateTime)
	}

	// An OCI HelmRepository is only a URL: there is nothing to fetch, so no condition either.
	if o.Kind == "HelmRepository" && o.Spec.Type == "oci" && len(st.Conditions) == 0 && !o.Spec.Suspend {
		s.Ready, s.Level = "", healthOK
	}

	return s
}

// fluxIcon names the icon from the catalog: by chart, then by name.
func fluxIcon(words ...string) (icon, remote string) {
	catalog := loadAppCatalog()

	for _, w := range words {
		if w == "" {
			continue
		}

		switch id := catalog.byWord(w); {
		case id.app != nil && id.app.hasIcon():
			return id.app.ID, ""
		case id.slug != "":
			return "", id.slug
		}
	}

	return "", ""
}
