package ichorgo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube/argocd"
)

// Argo CD is read and driven through its custom resources (argoproj.io Applications), with
// the admin kubeconfig Talos issues, the way `argocd --core` works: the logic is in
// go/kube/argocd. Here are the exported entry points, what comes from the rest of the facade
// (a pod's status, the controller's image, the icon catalog, the sign-in error, the privacy
// mask, the audit log) and the names the other kube_* files call.

const (
	groupArgo             = argocd.Group
	argoTrackingID        = argocd.TrackingID
	argoInstanceLabel     = argocd.InstanceLabel
	argoFreezesAnnotation = argocd.FreezesAnnotation
	argoActionSync        = argocd.ActionSync
	argoActionTerminate   = argocd.ActionTerminate
)

type (
	argoStatus          = argocd.Status
	argoApp             = argocd.App
	argoAppSet          = argocd.AppSet
	argoAppSetCondition = argocd.AppSetCondition
	argoOwner           = argocd.Owner
	argoSrc             = argocd.Source
	argoDest            = argocd.Dest
	argoAuto            = argocd.AutoSync
	argoOperation       = argocd.Operation
	argoResult          = argocd.Result
	argoCondition       = argocd.Condition
	argoResource        = argocd.Resource
	argoHistory         = argocd.History
	argoObject          = argocd.Object
	argoResourceStatus  = argocd.ResourceStatus
	argoProjectObject   = argocd.ProjectObject
	argoWindowObject    = argocd.WindowObject
	argoWindowEntry     = argocd.WindowEntry
	argoSyncOptions     = argocd.SyncOptions
	argoFreezeOptions   = argocd.FreezeOptions
)

var (
	argoSourcesOf    = argocd.SourcesOf
	argoAppKeys      = argocd.AppKeys
	argoWindowsPatch = argocd.WindowsPatch
	newArgoFreeze    = argocd.NewFreeze
	mapArgoCD        = argocd.Map
	mapArgoApp       = argocd.MapApp
	mapArgoProjects  = argocd.MapProjects
)

// KubeArgoCD lists the Argo CD Applications of every namespace (os:admin), with their sync and
// health, the running operation and its sync-wave progress, the resources, the deployment
// history, and the pods that are not ready next to an unhealthy app (so the app can point at
// a node). "installed" is false when the cluster serves no argoproj.io Applications.
// kubeServer: see KubePods.
func KubeArgoCD(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() argoStatus { return demoArgoCD(time.Now()) }, readArgoCD)
}

func readArgoCD(ctx context.Context, k *kubeClient) (argoStatus, error) {
	out, err := argocd.Read(ctx, argoEnv(k))
	if err != nil {
		return argoStatus{}, err
	}

	// So a namespace picked from the masked listing (a freeze) maps back to the real one.
	namespaces := namespacesOf(out.Apps, func(a argoApp) string { return a.Destination.Namespace })
	namespaces = append(namespaces, namespacesOf(out.Apps, func(a argoApp) string { return a.Namespace })...)
	privacy.learnNamespaces(namespaces)

	return out, nil
}

// argoEnv is what go/kube/argocd needs of k and of the rest of the facade.
func argoEnv(k *kubeClient) argocd.Env {
	return argocd.Env{
		Client: k.api(),
		Error:  kubeError,
		Version: func(ctx context.Context) string {
			pods, _ := listDSPods(ctx, k, "app.kubernetes.io/name=argocd-application-controller")

			return argoVersion(pods)
		},
		UnhealthyPods: func(ctx context.Context, namespaces []string) ([]kubePod, error) {
			return unhealthyPods(ctx, k, namespaces)
		},
		Icon: argoAppIcon,
	}
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

// argoAppIcon is the icon the app names in its annotations or labels, else the catalog's.
func argoAppIcon(a argoApp, annotations, labels map[string]string) (icon, remote, url string) {
	if custom, ok := customIcon(annotations, labels); ok {
		return custom.icon, custom.remote, custom.url
	}

	icon, remote = argoIcon(a)

	return icon, remote, ""
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

// KubeArgoAction runs an action on the Argo CD Application namespace/name (os:admin):
// refresh, hardRefresh, sync, terminate, autoSyncOn, autoSyncOff or rollback. optionsJSON
// holds the sync options (prune, dryRun, force, applyOutOfSyncOnly, serverSideApply, replace,
// resources) and, for a rollback, historyId; "" for none. Spec changes (auto-sync, rollback)
// are refused on an app an ApplicationSet or a parent app owns: the owner would revert them.
// kubeServer: see KubePods.
func KubeArgoAction(configYAML, contextName, kubeServer, namespace, name, action, optionsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "argo-" + action, Namespace: namespace, Object: "Application/" + name, Params: strings.TrimSpace(optionsJSON)})

	if !argocd.IsAction(action) {
		return fmt.Errorf("unsupported Argo CD action %q", action)
	}

	if err := validateKubeName("application", namespace, name); err != nil {
		return err
	}

	opts, err := argocd.ParseSyncOptions(optionsJSON)
	if err != nil {
		return err
	}

	return runArgoAction(kubeTarget{configYAML, contextName, kubeServer}, namespace, name, action, opts)
}

// runArgoAction is KubeArgoAction once its arguments are unmasked and checked: refused in
// the demo, one patch otherwise. ArgoSyncRun.Terminate goes through it too.
func runArgoAction(target kubeTarget, namespace, name, action string, opts argoSyncOptions) error {
	return kubeMutate(target, func(ctx context.Context, k *kubeClient) error {
		return argocd.Act(ctx, k.api(), namespace, name, action, opts, time.Now())
	})
}

// KubeArgoFreeze changes the sync windows of the Argo CD AppProject namespace/project
// (os:admin): freeze (a deny window from now for minutes, on applications or namespaces),
// extend (an Ichor freeze by minutes), unfreeze (remove a window; one Ichor did not create
// needs fromGit) or clearExpired (drop Ichor's ended freezes, which every change also does).
// optionsJSON holds argocd.FreezeOptions. kubeServer: see KubePods.
func KubeArgoFreeze(configYAML, contextName, kubeServer, namespace, project, action, optionsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, project = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(project))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "argo-" + action, Namespace: namespace, Object: "AppProject/" + project})

	if !argocd.IsFreezeAction(action) {
		return fmt.Errorf("unsupported Argo CD freeze action %q", action)
	}

	if err := validateKubeName("project", namespace, project); err != nil {
		return err
	}

	opts, err := argocd.ParseFreezeOptions(optionsJSON)
	if err != nil {
		return err
	}

	for i, s := range opts.Applications {
		opts.Applications[i] = privacy.reveal(strings.TrimSpace(s))
	}

	for i, s := range opts.Namespaces {
		opts.Namespaces[i] = privacy.revealNamespace(strings.TrimSpace(s))
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return argocd.Freeze(ctx, k.api(), namespace, project, action, opts, time.Now())
	})
}
