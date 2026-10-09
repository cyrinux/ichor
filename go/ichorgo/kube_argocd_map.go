package ichorgo

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
)

// The fields of argoproj.io/v1alpha1 objects the app reads.

type argoObject struct {
	Metadata struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		ResourceVersion string            `json:"resourceVersion"`
		Labels          map[string]string `json:"labels"`
		Annotations     map[string]string `json:"annotations"`
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec      argoSpec        `json:"spec"`
	Operation json.RawMessage `json:"operation"` // a requested operation not picked up yet
	Status    struct {
		Resources []argoResourceStatus `json:"resources"`
		Sync      struct {
			Status    string   `json:"status"`
			Revision  string   `json:"revision"`
			Revisions []string `json:"revisions"`
		} `json:"sync"`
		Health         argoHealth          `json:"health"`
		History        []argoHistoryObject `json:"history"`
		Conditions     []argoCondition     `json:"conditions"`
		ReconciledAt   string              `json:"reconciledAt"`
		OperationState *argoOperationState `json:"operationState"`
		Summary        struct {
			Images       []string `json:"images"`
			ExternalURLs []string `json:"externalURLs"`
		} `json:"summary"`
	} `json:"status"`
}

type argoSpec struct {
	Project     string             `json:"project"`
	Source      *argoSourceObject  `json:"source"`
	Sources     []argoSourceObject `json:"sources"`
	Destination argoDest           `json:"destination"`
	// IgnoreDifferences are the fields Argo CD leaves out of its comparison.
	IgnoreDifferences []argoIgnoreDifference `json:"ignoreDifferences"`
	SyncPolicy        *struct {
		Automated *struct {
			Prune    bool  `json:"prune"`
			SelfHeal bool  `json:"selfHeal"`
			Enabled  *bool `json:"enabled"` // Argo CD ≥ 3.1; absent means enabled
		} `json:"automated"`
		SyncOptions []string `json:"syncOptions"`
	} `json:"syncPolicy"`
}

// argoIgnoreDifference is one spec.ignoreDifferences entry: which objects (group and kind,
// optionally one name and namespace) and which fields of them.
type argoIgnoreDifference struct {
	Group                 string   `json:"group"`
	Kind                  string   `json:"kind"`
	Name                  string   `json:"name"`
	Namespace             string   `json:"namespace"`
	JSONPointers          []string `json:"jsonPointers"`
	JQPathExpressions     []string `json:"jqPathExpressions"`
	ManagedFieldsManagers []string `json:"managedFieldsManagers"`
}

type argoSourceObject struct {
	RepoURL        string `json:"repoURL"`
	Path           string `json:"path"`
	Chart          string `json:"chart"`
	TargetRevision string `json:"targetRevision"`
}

type argoHealth struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type argoResourceStatus struct {
	Group           string      `json:"group"`
	Version         string      `json:"version"`
	Kind            string      `json:"kind"`
	Namespace       string      `json:"namespace"`
	Name            string      `json:"name"`
	Status          string      `json:"status"`
	Health          *argoHealth `json:"health"`
	Hook            bool        `json:"hook"`
	RequiresPruning bool        `json:"requiresPruning"`
	SyncWave        int         `json:"syncWave"`
}

type argoInitiator struct {
	Username  string `json:"username"`
	Automated bool   `json:"automated"`
}

type argoHistoryObject struct {
	ID          int64           `json:"id"`
	Revision    string          `json:"revision"`
	Revisions   []string        `json:"revisions"`
	DeployedAt  string          `json:"deployedAt"`
	Source      json.RawMessage `json:"source"`
	Sources     json.RawMessage `json:"sources"`
	InitiatedBy argoInitiator   `json:"initiatedBy"`
}

type argoOperationState struct {
	Operation struct {
		Sync *struct {
			Revision  string            `json:"revision"`
			DryRun    bool              `json:"dryRun"`
			Resources []argoResourceRef `json:"resources"` // a selective sync
		} `json:"sync"`
		InitiatedBy argoInitiator `json:"initiatedBy"`
	} `json:"operation"`
	Phase      string          `json:"phase"`
	Message    string          `json:"message"`
	StartedAt  string          `json:"startedAt"`
	FinishedAt string          `json:"finishedAt"`
	RetryCount int             `json:"retryCount"`
	SyncResult *argoSyncResult `json:"syncResult"`
}

type argoAppSetObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

type argoProjectObject struct {
	Metadata struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		ResourceVersion string            `json:"resourceVersion"`
		Labels          map[string]string `json:"labels"`
		Annotations     map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Description string            `json:"description"`
		SyncWindows []json.RawMessage `json:"syncWindows"`
	} `json:"spec"`
}

// Conditions that mean the app cannot be synced or compared as it stands.
var argoErrorConditions = []string{"ComparisonError", "InvalidSpecError", "SyncError", "UnknownError"}

func mapArgoApp(o argoObject, apps map[string]bool) argoApp {
	st := &o.Status
	a := argoApp{
		Namespace: o.Metadata.Namespace, Name: o.Metadata.Name, Project: o.Spec.Project,
		Owner:  argoOwnerOf(o, apps),
		Health: cmp.Or(st.Health.Status, "Unknown"), HealthMessage: st.Health.Message,
		Sync: cmp.Or(st.Sync.Status, "Unknown"), Revision: st.Sync.Revision,
		Refreshing:  o.Metadata.Annotations[argoRefreshAnnotation],
		Sources:     []argoSrc{},
		Destination: o.Spec.Destination,
		SyncOptions: []string{},
		Conditions:  []argoCondition{},
		Images:      orEmpty(st.Summary.Images), ExternalURLs: orEmpty(st.Summary.ExternalURLs),
		UnhealthyPods: []kubePod{},
		ReconciledAt:  unixMilli(st.ReconciledAt),
	}

	if a.Revision == "" && len(st.Sync.Revisions) > 0 {
		a.Revision = st.Sync.Revisions[0]
	}

	sources := argoSourcesOf(o.Spec)
	for _, s := range sources {
		a.Sources = append(a.Sources, argoSrc{Repo: s.RepoURL, RepoURL: argoRepoWebURL(s.RepoURL), Path: s.Path, Chart: s.Chart, TargetRevision: s.TargetRevision})
	}

	a.RevisionURL = argoRevisionURL(sources, st.Sync.Revisions, a.Revision)

	if p := o.Spec.SyncPolicy; p != nil {
		a.SyncOptions = orEmpty(p.SyncOptions)
		if auto := p.Automated; auto != nil {
			a.AutoSync = argoAuto{Enabled: auto.Enabled == nil || *auto.Enabled, Prune: auto.Prune, SelfHeal: auto.SelfHeal}
		}
	}

	for _, c := range st.Conditions {
		a.Conditions = append(a.Conditions, argoCondition{Type: c.Type, Message: c.Message})
	}

	results := argoSyncResults(st.OperationState)
	a.Resources = argoResources(st.Resources, results)
	a.Operation = argoOp(st.OperationState, a.Resources, results)
	if a.Operation != nil {
		a.Operation.RevisionURL = argoRevisionURL(sources, nil, a.Operation.Revision)
	}

	a.History = argoHistoryOf(st.History)
	if custom, ok := customIcon(o.Metadata.Annotations, o.Metadata.Labels); ok {
		a.Icon, a.RemoteIcon, a.IconURL = custom.icon, custom.remote, custom.url
	} else {
		a.Icon, a.RemoteIcon = argoIcon(a)
	}
	a.Level = argoLevel(a)

	return a
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
}

// argoOwnerOf finds who writes the app's spec: an ApplicationSet (owner reference), or a
// parent app that manages it as one of its resources (tracking annotation or instance label
// naming another Application). Changing an owned app's spec is reverted by its owner.
func argoOwnerOf(o argoObject, apps map[string]bool) *argoOwner {
	for _, ref := range o.Metadata.OwnerReferences {
		if ref.Kind == "ApplicationSet" {
			return &argoOwner{Kind: ref.Kind, Name: ref.Name}
		}
	}

	self := o.Metadata.Namespace + "/" + o.Metadata.Name

	parent := ""
	if id := o.Metadata.Annotations[argoTrackingID]; id != "" {
		// "<app>:<group>/<kind>:<namespace>/<name>"; <app> is "<namespace>_<name>" for apps
		// outside the control-plane namespace.
		parent, _, _ = strings.Cut(id, ":")
	} else {
		parent = o.Metadata.Labels[argoInstanceLabel]
	}

	if parent == "" {
		return nil
	}

	for _, key := range argoAppKeys(parent, o.Metadata.Namespace) {
		if key != self && apps[key] {
			_, name, _ := strings.Cut(key, "/")

			return &argoOwner{Kind: "Application", Name: name}
		}
	}

	return nil
}

// argoAppKeys are the namespace/name keys an app reference may stand for.
func argoAppKeys(ref, namespace string) []string {
	if ns, name, ok := strings.Cut(ref, "_"); ok {
		return []string{ns + "/" + name, namespace + "/" + ref}
	}

	return []string{namespace + "/" + ref}
}

type argoKey struct{ group, kind, namespace, name string }

type argoResultEntry struct {
	status, message, hookPhase string
	hook                       bool
}

func argoSyncResults(op *argoOperationState) map[argoKey]argoResultEntry {
	out := map[argoKey]argoResultEntry{}
	if op == nil || op.SyncResult == nil {
		return out
	}

	for _, r := range op.SyncResult.Resources {
		out[argoKey{r.Group, r.Kind, r.Namespace, r.Name}] = argoResultEntry{r.Status, r.Message, r.HookPhase, r.HookType != ""}
	}

	return out
}

func argoResources(list []argoResourceStatus, results map[argoKey]argoResultEntry) []argoResource {
	out := make([]argoResource, 0, len(list))

	for _, r := range list {
		res := argoResource{
			Group: r.Group, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name, Sync: r.Status,
			Wave: r.SyncWave, Hook: r.Hook, Prune: r.RequiresPruning,
			SyncResult: results[argoKey{r.Group, r.Kind, r.Namespace, r.Name}].status,
		}

		if r.Health != nil {
			res.Health, res.HealthMessage = r.Health.Status, r.Health.Message
		}

		out = append(out, res)
	}

	slices.SortStableFunc(out, func(a, b argoResource) int { return a.Wave - b.Wave })

	return out
}

// argoOp sums the operation up. Progress counts the app's resources (only the selected ones
// for a selective sync) the operation already synced or pruned, or that were in sync and left
// alone, plus its hooks once they ran; the current wave is the lowest one with a resource
// still to sync.
func argoOp(op *argoOperationState, resources []argoResource, results map[argoKey]argoResultEntry) *argoOperation {
	if op == nil {
		return nil
	}

	out := &argoOperation{
		Phase: op.Phase, Message: op.Message, RetryCount: op.RetryCount,
		StartedAt: unixMilli(op.StartedAt), FinishedAt: unixMilli(op.FinishedAt),
		InitiatedBy: op.Operation.InitiatedBy.Username, Waves: []int{}, Failed: []argoResult{},
	}

	if op.Operation.InitiatedBy.Automated {
		out.InitiatedBy = "automated"
	}

	if s := op.Operation.Sync; s != nil {
		out.Revision, out.DryRun = s.Revision, s.DryRun
	}

	if op.SyncResult != nil && op.SyncResult.Revision != "" {
		out.Revision = op.SyncResult.Revision
	}

	waveSet := map[int]bool{}
	pending := map[int]bool{}

	var selected map[argoKey]bool
	if s := op.Operation.Sync; s != nil && len(s.Resources) > 0 {
		selected = map[argoKey]bool{}
		for _, r := range s.Resources {
			selected[argoKey{r.Group, r.Kind, r.Namespace, r.Name}] = true
		}
	}

	for _, r := range resources {
		if r.Hook || selected != nil && !selected[argoKey{r.Group, r.Kind, r.Namespace, r.Name}] {
			continue
		}

		out.Total++
		waveSet[r.Wave] = true

		switch {
		case r.SyncResult == "Synced" || r.SyncResult == "Pruned" || r.SyncResult == "PruneSkipped":
			out.Done++
		case r.SyncResult == "" && r.Sync == "Synced":
			// Already in sync and left alone (ApplyOutOfSyncOnly): nothing to wait for.
			out.Done++
		default:
			pending[r.Wave] = true
		}
	}

	for _, r := range op.SyncResult.resourcesOrNil() {
		key := argoKey{r.Group, r.Kind, r.Namespace, r.Name}
		entry := results[key]

		if entry.hook {
			out.Total++
			if entry.hookPhase == "Succeeded" {
				out.Done++
			}
		}

		if r.Status == "SyncFailed" || entry.hookPhase == "Failed" || entry.hookPhase == "Error" {
			out.Failed = append(out.Failed, argoResult{Kind: r.Kind, Namespace: r.Namespace, Name: r.Name, Message: r.Message})
		}
	}

	for w := range waveSet {
		out.Waves = append(out.Waves, w)
	}

	slices.Sort(out.Waves)

	out.Wave = argoCurrentWave(out.Waves, pending)

	return out
}

func argoCurrentWave(waves []int, pending map[int]bool) int {
	for _, w := range waves {
		if pending[w] {
			return w
		}
	}

	if len(waves) > 0 {
		return waves[len(waves)-1]
	}

	return 0
}

type argoSyncResult struct {
	Revision  string                   `json:"revision"`
	Resources []argoSyncResultResource `json:"resources"`
}

func (r *argoSyncResult) resourcesOrNil() []argoSyncResultResource {
	if r == nil {
		return nil
	}

	return r.Resources
}

type argoSyncResultResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	HookType  string `json:"hookType"`
	HookPhase string `json:"hookPhase"`
}

func argoHistoryOf(list []argoHistoryObject) []argoHistory {
	out := []argoHistory{}

	for i := len(list) - 1; i >= 0 && len(out) < argoHistoryMax; i-- {
		h := list[i]
		e := argoHistory{ID: h.ID, Revision: h.Revision, DeployedAt: unixMilli(h.DeployedAt), InitiatedBy: h.InitiatedBy.Username}

		if e.Revision == "" && len(h.Revisions) > 0 {
			e.Revision = h.Revisions[0]
		}

		if h.InitiatedBy.Automated {
			e.InitiatedBy = "automated"
		}

		sources := argoHistorySources(h)
		if len(sources) > 0 {
			e.TargetRevision, e.Chart = sources[0].TargetRevision, sources[0].Chart
		}

		e.URL = argoRevisionURL(sources, h.Revisions, h.Revision)

		out = append(out, e)
	}

	return out
}

// argoHistorySources are the sources a deployment used: the single one, or the list of a
// multi-source app (paired by position with its revisions).
func argoHistorySources(h argoHistoryObject) []argoSourceObject {
	var single argoSourceObject
	if len(h.Source) > 0 && json.Unmarshal(h.Source, &single) == nil && single.RepoURL != "" {
		return []argoSourceObject{single}
	}

	var list []argoSourceObject
	if len(h.Sources) > 0 && json.Unmarshal(h.Sources, &list) == nil {
		return list
	}

	return nil
}

// argoIcon names the app's icon from the catalog: by its Helm chart, its name, then the
// images it runs.
func argoIcon(a argoApp) (icon, remote string) {
	catalog := loadAppCatalog()

	candidates := make([]identification, 0, len(a.Sources)+len(a.Images)+1)
	for _, s := range a.Sources {
		if s.Chart != "" {
			candidates = append(candidates, catalog.byWord(s.Chart))
		}
	}

	candidates = append(candidates, catalog.byWord(a.Name))
	for _, img := range a.Images {
		candidates = append(candidates, catalog.identify(parseImageRef(img)))
	}

	for _, id := range candidates {
		switch {
		case id.app != nil && id.app.hasIcon():
			return id.app.ID, ""
		case id.slug != "":
			return "", id.slug
		}
	}

	return "", ""
}

// argoLevel: critical when the app is broken or its last sync failed, warning while it
// drifts or rolls out, idle when suspended.
func argoLevel(a argoApp) string {
	failed := a.Operation != nil && (a.Operation.Phase == "Failed" || a.Operation.Phase == "Error")
	errored := slices.ContainsFunc(a.Conditions, func(c argoCondition) bool { return slices.Contains(argoErrorConditions, c.Type) })

	switch {
	case a.Health == "Degraded" || a.Health == "Missing" || failed || errored:
		return healthCritical
	case a.Sync != "Synced" || a.Health == "Progressing" || a.Health == "Unknown":
		return healthWarning
	case a.Health == "Suspended":
		return healthIdle
	default:
		return healthOK
	}
}
