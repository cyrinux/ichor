package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Argo CD is read and driven through its custom resources (argoproj.io Applications), with
// the admin kubeconfig Talos issues, the way `argocd --core` works: no Argo CD token and no
// access to argocd-server needed. See plans/argocd/README.md.

const (
	groupArgo = "argoproj.io"
	// argoRefreshAnnotation asks the application controller to compare the app with Git again
	// ("normal") or to regenerate its manifests too ("hard"); the controller removes it.
	argoRefreshAnnotation = "argocd.argoproj.io/refresh"
	argoTrackingID        = "argocd.argoproj.io/tracking-id"
	argoInstanceLabel     = "app.kubernetes.io/instance"
	// argoHistoryMax caps the deployments kept per app on the wire, newest first.
	argoHistoryMax = 10
)

type argoStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"` // the application controller's image tag, "" when unknown
	// AppSetsError is set when listing the ApplicationSets failed; the apps still show.
	AppSetsError string        `json:"appSetsError"`
	Apps         []argoApp     `json:"apps"`
	AppSets      []argoAppSet  `json:"appSets"`
	Projects     []argoProject `json:"projects"`
}

type argoApp struct {
	Namespace string     `json:"namespace"`
	Name      string     `json:"name"`
	Project   string     `json:"project"`
	Owner     *argoOwner `json:"owner"` // the ApplicationSet or parent app that writes its spec
	// Level sums health, sync and the last operation up for sorting and colouring:
	// critical|warning|ok|idle (suspended).
	Level         string    `json:"level"`
	Icon          string    `json:"icon,omitempty"`
	RemoteIcon    string    `json:"remoteIcon,omitempty"`
	Health        string    `json:"health"` // Healthy|Progressing|Degraded|Suspended|Missing|Unknown
	HealthMessage string    `json:"healthMessage"`
	Sync          string    `json:"sync"` // Synced|OutOfSync|Unknown
	Revision      string    `json:"revision"`
	Refreshing    string    `json:"refreshing"` // the pending refresh annotation: normal|hard|""
	Sources       []argoSrc `json:"sources"`
	Destination   argoDest  `json:"destination"`
	AutoSync      argoAuto  `json:"autoSync"`
	SyncOptions   []string  `json:"syncOptions"`
	// Operation is the running or last sync; nil when none ran since the app was created.
	Operation     *argoOperation  `json:"operation"`
	Conditions    []argoCondition `json:"conditions"`
	Resources     []argoResource  `json:"resources"`
	History       []argoHistory   `json:"history"`
	Images        []string        `json:"images"`
	ExternalURLs  []string        `json:"externalURLs"`
	UnhealthyPods []kubePod       `json:"unhealthyPods"` // pods of its namespace not ready
	ReconciledAt  int64           `json:"reconciledAt"`
}

type argoOwner struct {
	Kind string `json:"kind"` // ApplicationSet|Application
	Name string `json:"name"`
}

type argoSrc struct {
	Repo           string `json:"repo"`
	Path           string `json:"path"`
	Chart          string `json:"chart"`
	TargetRevision string `json:"targetRevision"`
}

type argoDest struct {
	Server    string `json:"server"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type argoAuto struct {
	Enabled  bool `json:"enabled"`
	Prune    bool `json:"prune"`
	SelfHeal bool `json:"selfHeal"`
}

type argoOperation struct {
	Phase       string `json:"phase"` // Running|Terminating|Succeeded|Failed|Error
	Message     string `json:"message"`
	StartedAt   int64  `json:"startedAt"`
	FinishedAt  int64  `json:"finishedAt"`
	InitiatedBy string `json:"initiatedBy"` // a user name, or "automated"
	Revision    string `json:"revision"`
	RetryCount  int    `json:"retryCount"`
	DryRun      bool   `json:"dryRun"`
	Done        int    `json:"done"`  // resources synced so far
	Total       int    `json:"total"` // resources of the app, hooks included once run
	// Wave is the lowest sync wave with resources not synced yet; Waves every wave of the app.
	Wave   int          `json:"wave"`
	Waves  []int        `json:"waves"`
	Failed []argoResult `json:"failed"`
}

type argoResult struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Message   string `json:"message"`
}

type argoCondition struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type argoResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Sync      string `json:"sync"`
	// Health is empty for kinds without a health check, and on Argo CD 3 by default, which
	// keeps per-resource health out of the Application.
	Health        string `json:"health"`
	HealthMessage string `json:"healthMessage"`
	Wave          int    `json:"wave"`
	Hook          bool   `json:"hook"`
	Prune         bool   `json:"prune"` // would be deleted by a sync with prune
	// SyncResult is the last operation's word on it: Synced|SyncFailed|Pruned|PruneSkipped|"".
	SyncResult string `json:"syncResult"`
}

type argoHistory struct {
	ID             int64  `json:"id"`
	Revision       string `json:"revision"`
	TargetRevision string `json:"targetRevision"`
	Chart          string `json:"chart"`
	DeployedAt     int64  `json:"deployedAt"`
	InitiatedBy    string `json:"initiatedBy"`
}

type argoAppSet struct {
	Namespace  string                `json:"namespace"`
	Name       string                `json:"name"`
	Level      string                `json:"level"` // worst level of its apps, critical on an error condition
	Apps       int                   `json:"apps"`
	Conditions []argoAppSetCondition `json:"conditions"`
}

type argoAppSetCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type argoProject struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SyncWindows int    `json:"syncWindows"`
}

// KubeArgoCD lists the Argo CD Applications of every namespace (os:admin), with their sync and
// health, the running operation and its sync-wave progress, the resources, the deployment
// history, and the pods that are not ready next to an unhealthy app (so the app can point at
// a node). "installed" is false when the cluster serves no argoproj.io Applications.
// kubeServer: see KubePods.
func KubeArgoCD(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoArgoCD(time.Now()))
	}

	res, err := withKube(kubeTarget{configYAML, contextName, kubeServer}, readArgoCD)
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

func readArgoCD(ctx context.Context, k *kubeClient) (argoStatus, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return argoStatus{}, err
	}

	notInstalled := argoStatus{Apps: []argoApp{}, AppSets: []argoAppSet{}, Projects: []argoProject{}}

	version, ok := groups[groupArgo]
	if !ok {
		return notInstalled, nil
	}

	base := "/apis/" + groupArgo + "/" + version

	var (
		apps     lhList[argoObject]
		sets     lhList[argoAppSetObject]
		projects lhList[argoProjectObject]
		ctrl     []dsPod
		errs     = make([]error, 3)
		wg       sync.WaitGroup
	)

	wg.Go(func() { errs[0] = k.get(ctx, base+"/applications", &apps) })
	wg.Go(func() { errs[1] = k.get(ctx, base+"/applicationsets", &sets) })
	wg.Go(func() { errs[2] = k.get(ctx, base+"/appprojects", &projects) })
	wg.Go(func() { ctrl, _ = listDSPods(ctx, k, "app.kubernetes.io/name=argocd-application-controller") })
	wg.Wait()

	switch {
	case isNotFound(errs[0]):
		// argoproj.io is also Argo Rollouts' and Argo Workflows' group: no Applications, no Argo CD.
		return notInstalled, nil
	case errs[0] != nil:
		return argoStatus{}, errs[0]
	}

	out := mapArgoCD(apps.Items, sets.Items, projects.Items)
	out.Version = argoVersion(ctrl)

	if errs[1] != nil && !isNotFound(errs[1]) {
		out.AppSetsError = sectionError(errs[1])
	}

	if slices.ContainsFunc(out.Apps, func(a argoApp) bool { return a.Level == healthCritical || a.Health == "Progressing" }) {
		if pods, err := listPods(ctx, k); err == nil {
			attachUnhealthyPods(out.Apps, pods.Pods)
		}
	}

	return out, nil
}

// argoVersion is the tag of the application controller's image ("v3.4.5").
func argoVersion(pods []dsPod) string {
	for _, p := range pods {
		for _, c := range p.Spec.Containers {
			if tag := parseImageRef(c.Image).Tag; tag != "" {
				return tag
			}
		}
	}

	return ""
}

// attachUnhealthyPods gives each critical or progressing app the pods of its destination
// namespace that are not healthy: an app's own pods carry no Argo CD marker by default, so
// the namespace is the link (and usually one app owns one namespace).
func attachUnhealthyPods(apps []argoApp, pods []kubePod) {
	byNamespace := map[string][]kubePod{}

	for _, p := range pods {
		if !p.Healthy {
			byNamespace[p.Namespace] = append(byNamespace[p.Namespace], p)
		}
	}

	for i := range apps {
		a := &apps[i]
		if a.Level != healthCritical && a.Health != "Progressing" {
			continue
		}

		if pods := byNamespace[a.Destination.Namespace]; len(pods) > 0 {
			a.UnhealthyPods = pods
		}
	}
}

func mapArgoCD(objects []argoObject, sets []argoAppSetObject, projects []argoProjectObject) argoStatus {
	out := argoStatus{Installed: true, Apps: make([]argoApp, 0, len(objects)), AppSets: []argoAppSet{}, Projects: []argoProject{}}

	names := map[string]bool{}
	for _, o := range objects {
		names[o.Metadata.Namespace+"/"+o.Metadata.Name] = true
	}

	for _, o := range objects {
		out.Apps = append(out.Apps, mapArgoApp(o, names))
	}

	slices.SortFunc(out.Apps, func(a, b argoApp) int {
		return cmp.Or(healthRank(a.Level)-healthRank(b.Level), strings.Compare(a.Name, b.Name), strings.Compare(a.Namespace, b.Namespace))
	})

	for _, s := range sets {
		out.AppSets = append(out.AppSets, mapArgoAppSet(s, out.Apps))
	}

	slices.SortFunc(out.AppSets, func(a, b argoAppSet) int {
		return cmp.Or(healthRank(a.Level)-healthRank(b.Level), strings.Compare(a.Name, b.Name))
	})

	for _, p := range projects {
		out.Projects = append(out.Projects, argoProject{
			Namespace: p.Metadata.Namespace, Name: p.Metadata.Name,
			Description: p.Spec.Description, SyncWindows: len(p.Spec.SyncWindows),
		})
	}

	slices.SortFunc(out.Projects, func(a, b argoProject) int { return strings.Compare(a.Name, b.Name) })

	return out
}

func mapArgoAppSet(s argoAppSetObject, apps []argoApp) argoAppSet {
	set := argoAppSet{Namespace: s.Metadata.Namespace, Name: s.Metadata.Name, Level: healthIdle, Conditions: []argoAppSetCondition{}}

	for _, a := range apps {
		if a.Owner == nil || a.Owner.Kind != "ApplicationSet" || a.Owner.Name != set.Name || a.Namespace != set.Namespace {
			continue
		}

		set.Apps++
		if healthRank(a.Level) < healthRank(set.Level) {
			set.Level = a.Level
		}
	}

	for _, c := range s.Status.Conditions {
		set.Conditions = append(set.Conditions, argoAppSetCondition{Type: c.Type, Status: c.Status, Message: c.Message})

		if c.Type == "ErrorOccurred" && c.Status == "True" {
			set.Level = healthCritical
		}
	}

	if set.Apps == 0 && set.Level == healthIdle && len(set.Conditions) > 0 {
		set.Level = healthOK
	}

	return set
}

func isNotFound(err error) bool {
	var apiErr *kubeAPIError

	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
}

// argoSourcesOf lists the app's sources (spec.sources, or the single spec.source).
func argoSourcesOf(spec argoSpec) []argoSourceObject {
	if len(spec.Sources) > 0 {
		return spec.Sources
	}

	if spec.Source != nil {
		return []argoSourceObject{*spec.Source}
	}

	return nil
}
