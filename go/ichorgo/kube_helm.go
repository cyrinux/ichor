package ichorgo

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube/helm"
)

// Helm releases, read-only: the logic is in go/kube/helm; here are the entry points the apps
// call, with their mask and their demo data.

// KubeHelmReleases lists the latest revision of each Helm release (in namespace, "" for all),
// as a JSON helm.ReleaseList sorted by namespace then name.
func KubeHelmReleases(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoHelmReleases, func(ctx context.Context, k *kubeClient) (helm.ReleaseList, error) {
		return helm.Releases(ctx, k.api(), namespace)
	})
}

// KubeHelmRelease reads one release: its latest revision (values, manifest, notes) and its
// history, as a JSON helm.ReleaseDetail.
func KubeHelmRelease(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("Helm release", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() helm.ReleaseDetail { return demoHelmRelease(namespace, name) }, func(ctx context.Context, k *kubeClient) (helm.ReleaseDetail, error) {
		return helm.Detail(ctx, k.api(), namespace, name)
	})
}

func demoHelmReleases() helm.ReleaseList {
	updated := time.Now().Add(-6 * 24 * time.Hour).Unix()

	return helm.ReleaseList{Releases: []helm.ReleaseSummary{
		{Name: "cert-manager", Namespace: "cert-manager", Revision: 4, Status: "deployed", Chart: "cert-manager", ChartVersion: "v1.18.2", AppVersion: "v1.18.2", Updated: updated},
		{Name: "ingress-nginx", Namespace: "ingress-nginx", Revision: 7, Status: "deployed", Chart: "ingress-nginx", ChartVersion: "4.13.0", AppVersion: "1.13.0", Updated: updated},
	}}
}

func demoHelmRelease(namespace, name string) helm.ReleaseDetail {
	for _, r := range demoHelmReleases().Releases {
		if r.Namespace == namespace && r.Name == name {
			day := int64(24 * 60 * 60)

			return helm.ReleaseDetail{
				ReleaseSummary: r, Description: "Upgrade complete", Values: "replicaCount: 2\n",
				Manifest: "# Demo cluster: no real manifest.\n",
				History: []helm.Revision{
					{Revision: r.Revision, Status: r.Status, Updated: r.Updated},
					{Revision: r.Revision - 1, Status: helm.StatusSuperseded, Updated: r.Updated - 9*day},
					{Revision: r.Revision - 2, Status: helm.StatusSuperseded, Updated: r.Updated - 30*day},
				},
			}
		}
	}

	return helm.ReleaseDetail{ReleaseSummary: helm.ReleaseSummary{Name: name, Namespace: namespace}, History: []helm.Revision{}}
}

// demoPreviousVersion is the chart version before v: its last number one lower.
func demoPreviousVersion(v string) string {
	i := strings.LastIndexByte(v, '.')
	if n, err := strconv.Atoi(v[i+1:]); err == nil && n > 0 {
		return v[:i+1] + strconv.Itoa(n-1)
	}

	return v
}
