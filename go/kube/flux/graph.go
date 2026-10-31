package flux

import (
	"cmp"
	"strings"

	"github.com/cyrinux/ichor/go/kube"
)

// The fields of the Flux v2 objects the app reads (kustomize v1, helm v2 and v2beta*,
// source v1 and v1beta2 all share them).

// Meta is an object's metadata.
type Meta struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ResourceVersion string            `json:"resourceVersion"`
	Labels          map[string]string `json:"labels"`
	Annotations     map[string]string `json:"annotations"`
}

// CrossRef points at another object; an empty Namespace is the object's own.
type CrossRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// CommonStatus is the status every Flux kind shares.
type CommonStatus struct {
	Conditions             kube.Conditions `json:"conditions"`
	LastHandledReconcileAt string          `json:"lastHandledReconcileAt"`
	LastAppliedRevision    string          `json:"lastAppliedRevision"`
	LastAttemptedRevision  string          `json:"lastAttemptedRevision"`
}

// KustomizationObject is a Kustomization as the API serves it.
type KustomizationObject struct {
	Metadata Meta `json:"metadata"`
	Spec     struct {
		SourceRef       CrossRef   `json:"sourceRef"`
		Path            string     `json:"path"`
		Interval        string     `json:"interval"`
		Suspend         bool       `json:"suspend"`
		Prune           bool       `json:"prune"`
		TargetNamespace string     `json:"targetNamespace"`
		NamePrefix      string     `json:"namePrefix"`
		NameSuffix      string     `json:"nameSuffix"`
		DependsOn       []CrossRef `json:"dependsOn"`
	} `json:"spec"`
	Status struct {
		CommonStatus
		Inventory *struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"inventory"`
	} `json:"status"`
}

// HelmReleaseObject is a HelmRelease as the API serves it.
type HelmReleaseObject struct {
	Metadata Meta `json:"metadata"`
	Spec     struct {
		Chart *struct {
			Spec struct {
				Chart       string   `json:"chart"`
				Version     string   `json:"version"`
				SourceRef   CrossRef `json:"sourceRef"`
				ValuesFiles []string `json:"valuesFiles"`
			} `json:"spec"`
		} `json:"chart"`
		ChartRef       *CrossRef `json:"chartRef"`
		DriftDetection *struct {
			Mode string `json:"mode"`
		} `json:"driftDetection"`
		Interval         string     `json:"interval"`
		Suspend          bool       `json:"suspend"`
		TargetNamespace  string     `json:"targetNamespace"`
		StorageNamespace string     `json:"storageNamespace"`
		DependsOn        []CrossRef `json:"dependsOn"`
	} `json:"spec"`
	Status struct {
		CommonStatus
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

// FollowRef is what a GitRepository or OCIRepository follows.
type FollowRef struct {
	Branch string `json:"branch"`
	Tag    string `json:"tag"`
	SemVer string `json:"semver"`
	Name   string `json:"name"`
	Commit string `json:"commit"`
	Digest string `json:"digest"`
}

// SourceObject is a source as the API serves it; Kind is set by the reader, as the items of
// a list do not carry it.
type SourceObject struct {
	Kind     string `json:"kind"`
	Metadata Meta   `json:"metadata"`
	Spec     struct {
		URL        string     `json:"url"`
		Type       string     `json:"type"` // a HelmRepository's "oci"
		Interval   string     `json:"interval"`
		Suspend    bool       `json:"suspend"`
		BucketName string     `json:"bucketName"`
		Endpoint   string     `json:"endpoint"`
		Ref        *FollowRef `json:"ref"`
		// A HelmChart's chart and version.
		Chart   string `json:"chart"`
		Version string `json:"version"`
	} `json:"spec"`
	Status struct {
		CommonStatus
		Artifact *struct {
			Revision       string `json:"revision"`
			LastUpdateTime string `json:"lastUpdateTime"`
		} `json:"artifact"`
	} `json:"status"`
}

// applyCommon fills what Kustomizations and HelmReleases share.
func applyCommon(a *App, m Meta, st CommonStatus, suspended bool, deps []CrossRef) {
	ready := st.Conditions.Get("Ready")

	a.Namespace, a.Name = m.Namespace, m.Name
	a.Ready, a.Reason, a.Message = cmp.Or(ready.Status, "Unknown"), ready.Reason, ready.Message
	a.Reconciling, a.Stalled, a.Suspended = st.Conditions.Is("Reconciling"), st.Conditions.Is("Stalled"), suspended
	a.Pending = pending(m, st)
	a.Revision, a.AttemptedRevision = st.LastAppliedRevision, st.LastAttemptedRevision
	a.ReconciledAt = kube.UnixMilli(ready.LastTransitionTime)
	a.Level = level(suspended, st.Conditions)
	a.Conditions = conditionsOf(st.Conditions)
	a.DependsOn = []string{}
	a.Resources = []Resource{}
	a.History = []History{}
	a.UnhealthyPods = []kube.Pod{}

	for _, d := range deps {
		a.DependsOn = append(a.DependsOn, cmp.Or(d.Namespace, m.Namespace)+"/"+d.Name)
	}

	if name := m.Labels[OwnerName]; name != "" {
		a.Owner = &Ref{Kind: "Kustomization", Namespace: m.Labels[OwnerNamespace], Name: name}
	}
}

// pending: a reconcile was requested and the controller has not handled it yet.
func pending(m Meta, st CommonStatus) bool {
	req := m.Annotations[requestedAt]

	return req != "" && req != st.LastHandledReconcileAt
}

func conditionsOf(list kube.Conditions) []Condition {
	out := make([]Condition, 0, len(list))
	for _, c := range list {
		out = append(out, Condition{Type: c.Type, Status: c.Status, Reason: c.Reason, Message: c.Message, At: kube.UnixMilli(c.LastTransitionTime)})
	}

	return out
}

func sourceRef(ref CrossRef, namespace string, urls map[Ref]string) (*Ref, string) {
	if ref.Name == "" {
		return nil, ""
	}

	r := Ref{Kind: ref.Kind, Namespace: cmp.Or(ref.Namespace, namespace), Name: ref.Name}

	return &r, urls[r]
}

func mapKustomization(o KustomizationObject, urls map[Ref]string) App {
	a := App{Kind: "Kustomization", Path: o.Spec.Path, Interval: o.Spec.Interval, Prune: o.Spec.Prune, TargetNamespace: o.Spec.TargetNamespace}
	applyCommon(&a, o.Metadata, o.Status.CommonStatus, o.Spec.Suspend, o.Spec.DependsOn)
	a.Source, a.SourceURL = sourceRef(o.Spec.SourceRef, o.Metadata.Namespace, urls)

	namespaces := map[string]bool{}

	if inv := o.Status.Inventory; inv != nil {
		for _, e := range inv.Entries {
			r, ok := parseInventoryID(e.ID)
			if !ok {
				continue
			}

			if r.Namespace != "" && !namespaces[r.Namespace] {
				namespaces[r.Namespace] = true
				a.namespaces = append(a.namespaces, r.Namespace)
			}

			if len(a.Resources) < resourcesMax {
				a.Resources = append(a.Resources, r)
			}
		}
	}

	if a.TargetNamespace != "" {
		a.namespaces = []string{a.TargetNamespace}
	}

	return a
}

// parseInventoryID reads "<namespace>_<name>_<group>_<kind>" (namespace and group may be
// empty; Kubernetes names hold no "_").
func parseInventoryID(id string) (Resource, bool) {
	parts := strings.Split(id, "_")
	if len(parts) != 4 || parts[1] == "" || parts[3] == "" {
		return Resource{}, false
	}

	return Resource{Namespace: parts[0], Name: parts[1], Group: parts[2], Kind: parts[3]}, true
}

func mapHelmRelease(o HelmReleaseObject, urls map[Ref]string) App {
	a := App{Kind: "HelmRelease", Interval: o.Spec.Interval, TargetNamespace: o.Spec.TargetNamespace}
	applyCommon(&a, o.Metadata, o.Status.CommonStatus, o.Spec.Suspend, o.Spec.DependsOn)
	a.Failures = o.Status.InstallFailures + o.Status.UpgradeFailures
	a.namespaces = []string{cmp.Or(o.Spec.TargetNamespace, o.Metadata.Namespace)}

	switch {
	case o.Spec.ChartRef != nil:
		a.Source, a.SourceURL = sourceRef(*o.Spec.ChartRef, o.Metadata.Namespace, urls)
	case o.Spec.Chart != nil:
		c := o.Spec.Chart.Spec
		a.Chart, a.ChartVersion = c.Chart, c.Version
		a.Source, a.SourceURL = sourceRef(c.SourceRef, o.Metadata.Namespace, urls)
	}

	h := o.Status.History
	for i := 0; i < len(h) && len(a.History) < historyMax; i++ {
		a.History = append(a.History, History{
			Version: h[i].Version, ChartVersion: h[i].ChartVersion, AppVersion: h[i].AppVersion, Status: h[i].Status,
			DeployedAt: kube.UnixMilli(cmp.Or(h[i].LastDeployed, h[i].FirstDeployed)),
		})
	}

	if len(h) > 0 {
		// helm v2 keeps the release history instead of lastAppliedRevision.
		a.Revision = cmp.Or(a.Revision, h[0].ChartVersion)
		a.Chart = cmp.Or(a.Chart, h[0].ChartName)
	}

	applyDrift(&a, o)

	return a
}

// driftCondition is the condition the helm-controller sets when it compared the release
// with the cluster: True (reason DriftDetected) when objects changed.
const driftCondition = "Drifted"

// driftMessageLimit bounds the drift message shown, as the limit on an event's.
const driftMessageLimit = 400

// applyDrift fills what the helm-controller found about drift, never parsed from events.
func applyDrift(a *App, o HelmReleaseObject) {
	if d := o.Spec.DriftDetection; d != nil {
		a.DriftMode = d.Mode
	}

	c := o.Status.Conditions.Get(driftCondition)
	if c.Status != "True" {
		return
	}

	msg := strings.TrimSpace(c.Message)
	if len(msg) > driftMessageLimit {
		msg = strings.ToValidUTF8(msg[:driftMessageLimit], "") + "…"
	}

	a.Drifted, a.DriftMessage, a.DriftAt = true, msg, kube.UnixMilli(c.LastTransitionTime)
}

func mapSource(o SourceObject) Source {
	st := o.Status
	ready := st.Conditions.Get("Ready")

	s := Source{
		Kind: o.Kind, Namespace: o.Metadata.Namespace, Name: o.Metadata.Name,
		URL: o.Spec.URL, Interval: o.Spec.Interval, Suspended: o.Spec.Suspend,
		Ready: cmp.Or(ready.Status, "Unknown"), Reason: ready.Reason, Message: ready.Message,
		Reconciling: st.Conditions.Is("Reconciling"), Pending: pending(o.Metadata, st.CommonStatus),
		Level: level(o.Spec.Suspend, st.Conditions),
	}

	if s.URL == "" && o.Spec.BucketName != "" {
		s.URL = strings.TrimSuffix(o.Spec.Endpoint, "/") + "/" + o.Spec.BucketName
	}

	if r := o.Spec.Ref; r != nil {
		s.Ref = cmp.Or(r.Name, r.Commit, r.Digest, r.SemVer, r.Tag, r.Branch)
	}

	if a := st.Artifact; a != nil {
		s.Revision, s.FetchedAt = a.Revision, kube.UnixMilli(a.LastUpdateTime)
	}

	// An OCI HelmRepository is only a URL: there is nothing to fetch, so no condition either.
	if o.Kind == "HelmRepository" && o.Spec.Type == "oci" && len(st.Conditions) == 0 && !o.Spec.Suspend {
		s.Ready, s.Level = "", kube.HealthOK
	}

	return s
}
