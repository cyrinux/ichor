package ichorgo

import (
	"context"
	"strings"

	"github.com/cyrinux/ichor/go/kube/argocd"
)

// The Argo CD diff: what syncing an Application now would change, read from the comparison
// Argo CD keeps in its Redis (go/kube/argocd/diff.go). Here are the entry point, the demo and
// the port-forward it reaches Redis through.

// KubeArgoDiff compares the Argo CD Application namespace/name with what a sync would apply
// now (os:admin), as Argo CD itself compared it last: per object created, changed (with a
// unified diff), deleted (pruned), unchanged, ignored (a hook) or error. Secret values never
// show. kubeServer: see KubePods.
func KubeArgoDiff(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("application", namespace, name); err != nil {
		return "", err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return toJSON(demoArgoDiff(namespace, name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), argocd.DiffTimeout)
	defer cancel()

	res, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (gitOpsDiff, error) {
		env := argoEnv(k)
		env.Dial = k.dialPodPort

		return argocd.Diff(ctx, env, namespace, name)
	})
	if err != nil {
		return "", err
	}

	return toJSON(res)
}
