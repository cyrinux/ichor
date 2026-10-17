package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// The actions of the `kubectl argo rollouts` plugin, as the same merge patches: a Rollout's
// spec on the object, its status on the status subresource (where the API server keeps it).

const (
	argoRolloutPromote     = "promote"
	argoRolloutPromoteFull = "promoteFull"
	argoRolloutAbort       = "abort"
	argoRolloutRetry       = "retry"
	argoRolloutPause       = "pause"
)

var argoRolloutActions = []string{argoRolloutPromote, argoRolloutPromoteFull, argoRolloutAbort, argoRolloutRetry, argoRolloutPause}

var errArgoRolloutsMissing = &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Argo Rollouts is not installed"}

// argoRolloutPatch is one merge patch: on the status subresource, or on the object.
type argoRolloutPatch struct {
	status bool
	body   map[string]any
}

// KubeArgoRolloutAction runs action on the Rollout namespace/name (os:admin): promote (the
// next step, or the paused blue-green), promoteFull (skip the remaining steps), abort (the
// stable version takes the traffic back), retry (after an abort) or pause.
// kubeServer: see KubePods.
func KubeArgoRolloutAction(configYAML, contextName, kubeServer, namespace, name, action string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "argo-rollout-" + action, Namespace: namespace, Object: "Rollout/" + name})

	if !slices.Contains(argoRolloutActions, action) {
		return fmt.Errorf("unsupported Argo Rollouts action %q", action)
	}

	if err := validateKubeName("rollout", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return argoRolloutAction(ctx, k, namespace, name, action)
	})
}

func argoRolloutAction(ctx context.Context, k *kubeClient, namespace, name, action string) error {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return err
	}

	version := groups[groupArgo]
	if version == "" {
		return errArgoRolloutsMissing
	}

	path := "/apis/" + groupArgo + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/rollouts/" + url.PathEscape(name)

	var obj argoRolloutObject
	if err := k.get(ctx, path, &obj); err != nil {
		return err
	}

	patches, err := argoRolloutPatches(obj, action)
	if err != nil {
		return err
	}

	for _, p := range patches {
		target := path
		if p.status {
			target += "/status"
		}

		err := k.patch(ctx, target, "application/merge-patch+json", p.body, nil)
		// A controller older than 0.10 has no status subresource: the object takes it.
		if p.status && isNotFound(err) {
			err = k.patch(ctx, path, "application/merge-patch+json", p.body, nil)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// argoRolloutPatches are the plugin's patches for action on r, in order, or why it is refused.
func argoRolloutPatches(r argoRolloutObject, action string) ([]argoRolloutPatch, error) {
	st := r.Status
	blueGreen := r.Spec.Strategy.BlueGreen != nil
	// Nothing to promote or abort: the stable version already runs.
	settled := st.CurrentPodHash != "" && st.CurrentPodHash == st.StableRS

	unpause := argoRolloutPatch{body: map[string]any{"spec": map[string]any{"paused": false}}}

	switch action {
	case argoRolloutPause:
		return []argoRolloutPatch{{body: map[string]any{"spec": map[string]any{"paused": true}}}}, nil
	case argoRolloutRetry:
		return []argoRolloutPatch{{status: true, body: map[string]any{"status": map[string]any{"abort": false}}}}, nil
	case argoRolloutAbort:
		if blueGreen && settled {
			return nil, argoRolloutNothing("abort")
		}

		return []argoRolloutPatch{{status: true, body: map[string]any{"status": map[string]any{"abort": true}}}}, nil
	case argoRolloutPromoteFull:
		var out []argoRolloutPatch
		if r.Spec.Paused {
			out = append(out, unpause)
		}

		if !settled {
			out = append(out, argoRolloutPatch{status: true, body: map[string]any{"status": map[string]any{"promoteFull": true}}})
		}

		if len(out) == 0 {
			return nil, argoRolloutNothing("promote")
		}

		return out, nil
	}

	// promote
	if blueGreen && settled && !r.Spec.Paused {
		return nil, argoRolloutNothing("promote")
	}

	// The plugin's order: the status first, then the spec.
	var out []argoRolloutPatch

	canary := r.Spec.Strategy.Canary
	nextStep := func() int { return min(*st.CurrentStepIndex+1, len(canary.Steps)) }

	switch {
	case len(st.PauseConditions) > 0:
		status := map[string]any{"pauseConditions": nil}
		// An inconclusive analysis pauses the controller too; the plugin also moves to the
		// next step, or a pause step next would keep the rollout stuck.
		if st.ControllerPause {
			status["controllerPause"] = false
			if canary != nil && st.CurrentStepIndex != nil {
				status["currentStepIndex"] = nextStep()
			}
		}

		out = append(out, argoRolloutPatch{status: true, body: map[string]any{"status": status}})
	case canary != nil && st.CurrentStepIndex != nil:
		// Mid-step (an analysis running, no pause): the next step, as the plugin skips it.
		out = append(out, argoRolloutPatch{status: true, body: map[string]any{"status": map[string]any{"pauseConditions": nil, "currentStepIndex": nextStep()}}})
	}

	if r.Spec.Paused {
		out = append(out, unpause)
	}

	if len(out) == 0 {
		return nil, &kubeAPIError{Code: 409, Reason: "Conflict", Message: "nothing to promote: the rollout is not paused"}
	}

	return out, nil
}

func argoRolloutNothing(what string) error {
	return &kubeAPIError{Code: 409, Reason: "Conflict", Message: fmt.Sprintf("nothing to %s: the stable version already runs", what)}
}
