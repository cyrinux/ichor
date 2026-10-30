package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube/helm"
)

// `helm rollback` without the Helm SDK: the logic is in go/kube/helm; here are the entry points
// the apps call, with their mask, audit and demo.

// helmEnv is what go/kube/helm needs of k and of the domains still in the facade.
func helmEnv(k *kubeClient) helm.Env {
	return helm.Env{
		Client: k.api(),
		Error:  kubeError,
		Fatal:  followFatal,
		Suspend: func(ctx context.Context, namespace, name string, now time.Time) error {
			return fluxAction(ctx, k, "HelmRelease", namespace, name, fluxActionSuspend, now)
		},
		Rollout: func(ctx context.Context, kind, namespace, name string) (helm.RolloutState, error) {
			wk, err := findWorkloadKind(kind)
			if err != nil {
				return helm.RolloutState{}, err
			}

			st, err := rolloutStatus(ctx, k, wk, namespace, name)
			if err != nil {
				return helm.RolloutState{}, err
			}

			return helm.RolloutState{
				Ready: int(st.Workload.Ready), Desired: int(st.Workload.Desired),
				Done: st.Done, Failed: st.Failed, Manual: st.Manual, Stale: st.Stale,
			}, nil
		},
	}
}

// KubeHelmRollbackPlan says what rolling the Helm release namespace/name back to revision
// would do, without doing it (os:admin): every change is dry-run on the API server. revision
// 0 is the previous one, as for `helm rollback`. optionsJSON is {"force","historyMax"}
// ("" for neither). JSON helm.Plan. kubeServer: see KubePods.
func KubeHelmRollbackPlan(configYAML, contextName, kubeServer, namespace, name string, revision int, optionsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	opts, err := helm.ParseRollbackOptions(optionsJSON)
	if err != nil {
		return "", err
	}

	if err := validateHelmRollback(namespace, name, revision); err != nil {
		return "", err
	}

	demo := func() helm.Plan { return demoHelmRollbackPlan(namespace, name, revision, opts) }

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return toJSON(demo())
	}

	ctx, cancel := context.WithTimeout(context.Background(), helm.PlanTimeout)
	defer cancel()

	plan, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (helm.Plan, error) {
		return helm.PlanRollback(ctx, helmEnv(k), namespace, name, revision, opts)
	})
	if err != nil {
		return "", err
	}

	return toJSON(plan)
}

// KubeHelmRollback rolls the Helm release namespace/name back to revision (0: the previous
// one), after the same checks and dry run as KubeHelmRollbackPlan (os:admin). A HelmRelease
// managing it is suspended first. The chart's rollback hooks run around the changes, waited
// for (5 minutes each at most). On failure the new revision is recorded as failed, as Helm
// does, and the error names the object or the hook that stopped it. optionsJSON: see
// KubeHelmRollbackPlan. Old revisions are pruned (historyMax) only once the new one is
// deployed; a delete refused does not undo the rollback: the error then starts with
// "history not pruned:".
func KubeHelmRollback(configYAML, contextName, kubeServer, namespace, name string, revision int, optionsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	opts, optsErr := helm.ParseRollbackOptions(optionsJSON)

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "helm-rollback", Namespace: namespace, Object: "HelmRelease/" + name, Params: opts.AuditParams(revision)})

	if optsErr != nil {
		return optsErr
	}

	if err := validateHelmRollback(namespace, name, revision); err != nil {
		return err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return errDemoUnavailable
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The prune's error and a hook's stay out of kubeDo: see helm.Outcome.
	var outcome helm.Outcome

	err = kubeMutationError(kubeDo(ctx, target, func(ctx context.Context, k *kubeClient) error {
		var runErr error

		outcome, runErr = helm.Rollback(ctx, helmEnv(k), namespace, name, revision, opts)

		return runErr
	}))
	if err != nil {
		return errors.Join(err, outcome.Prune)
	}

	if outcome.Hook != nil {
		return outcome.Hook
	}

	// Rolled back: the audit entry and the apps get "history not pruned: ..." alone.
	return outcome.Prune
}

func validateHelmRollback(namespace, name string, revision int) error {
	if revision < 0 {
		return fmt.Errorf("invalid revision %d", revision)
	}

	return validateKubeName("Helm release", namespace, name)
}
