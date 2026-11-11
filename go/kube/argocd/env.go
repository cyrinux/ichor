package argocd

import (
	"context"
	"io"

	"github.com/cyrinux/ichor/go/kube"
)

// Env is what reading Argo CD needs: the client, and what comes from domains still in the
// facade.
type Env struct {
	kube.Client

	// Error is the message for an error of a Kubernetes call; nil is kube.Error. The facade's
	// also reduces a needed sign-in to its own message, which the apps recognise.
	Error func(error) error
	// Version is the Argo CD release the cluster runs, "" when unknown; nil is unknown.
	Version func(ctx context.Context) string
	// UnhealthyPods reads the pods of namespaces that are not healthy; nil shows none.
	UnhealthyPods func(ctx context.Context, namespaces []string) ([]kube.Pod, error)
	// Icon names an app's icon from the annotations and labels of its Application (a custom
	// icon), else from the catalog by its charts, name and images; nil leaves none.
	Icon func(a App, annotations, labels map[string]string) (icon, remote, url string)
	// Dial opens a stream to a pod's port (a port-forward), for Diff to reach Argo CD's Redis.
	Dial func(ctx context.Context, namespace, pod string, port int) (io.ReadWriteCloser, error)
}

func (e Env) version(ctx context.Context) string {
	if e.Version == nil {
		return ""
	}

	return e.Version(ctx)
}

// icons sets the icon of each app from its object.
func (e Env) icons(apps []App, objects []Object) {
	if e.Icon == nil {
		return
	}

	type meta struct{ annotations, labels map[string]string }

	byName := make(map[string]meta, len(objects))
	for _, o := range objects {
		byName[o.Metadata.Namespace+"/"+o.Metadata.Name] = meta{o.Metadata.Annotations, o.Metadata.Labels}
	}

	for i := range apps {
		m := byName[apps[i].Namespace+"/"+apps[i].Name]
		apps[i].Icon, apps[i].RemoteIcon, apps[i].IconURL = e.Icon(apps[i], m.annotations, m.labels)
	}
}

// sectionError is the message of a section that failed, for its "error" field.
func (e Env) sectionError(err error) string {
	message := e.Error
	if message == nil {
		message = kube.Error
	}

	return message(err).Error()
}
