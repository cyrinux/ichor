package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// A chart's pre-rollback and post-rollback hooks, run as Helm's execHook (pkg/action/hooks.go)
// runs them: per event, by weight then name; for each one the before-hook-creation policy
// (an object of the same name deleted, and gone, first), the object created, then waited for
// (a Job until Complete or Failed, a Pod until Succeeded or Failed, any other kind once
// created), then the hook-failed policy on a failure (and hook-succeeded on the hooks of the
// event that passed), or hook-succeeded on every hook of the event once they all passed,
// last one first. Hooks with no delete policy get before-hook-creation, as Helm gives them.
// Hook objects are not the release's: they never enter the change steps and are not stamped
// with its ownership. Kubernetes access: create, delete and get on each hook's kind (batch
// Jobs, Pods) in the release namespace.

const (
	helmHookPreRollback  = "pre-rollback"
	helmHookPostRollback = "post-rollback"

	helmHookBeforeCreation = "before-hook-creation"
	helmHookSucceeded      = "hook-succeeded"
	helmHookFailed         = "hook-failed"

	// helmHookTimeout bounds the wait for one hook (Helm's --timeout default is 5 minutes).
	helmHookTimeout = 5 * time.Minute
)

// helmHookPollInterval is how often a hook object is read while waited for (a variable for
// the tests).
var helmHookPollInterval = 2 * time.Second

// helmHook is one hook the rollback runs, in the plan. DeletePolicies are the effective ones
// (before-hook-creation when the chart sets none); Error is why the API server refused its
// dry run (a refused hook blocks the rollback).
type helmHook struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Event          string   `json:"event"` // "pre-rollback" or "post-rollback"
	Weight         int      `json:"weight"`
	DeletePolicies []string `json:"deletePolicies"`
	Error          string   `json:"error"`
}

// helmHookStep is a hook ready to run: its object as the target revision rendered it.
type helmHookStep struct {
	hook helmHook
	obj  helmObject
}

func (h helmHookStep) label() string {
	return h.hook.Event + " hook " + h.hook.Kind + " " + h.hook.Name
}

func (h helmHookStep) hasPolicy(policy string) bool {
	return slices.Contains(h.hook.DeletePolicies, policy)
}

// helmEventHooks are target's hooks firing on event, in Helm's order (hookByWeight, stable).
func helmEventHooks(target helmRelease, event string) []helmReleaseHook {
	out := []helmReleaseHook{}

	for _, h := range target.Hooks {
		if slices.Contains(h.Events, event) {
			out = append(out, h)
		}
	}

	slices.SortStableFunc(out, func(a, b helmReleaseHook) int {
		return cmp.Or(cmp.Compare(a.Weight, b.Weight), strings.Compare(a.Name, b.Name))
	})

	return out
}

// planHooks resolves and dry-runs the target revision's rollback hooks into the plan and
// the steps the run takes; the error says how many the API server refuses.
func (rb *helmRollback) planHooks(ctx context.Context, k *kubeClient, kinds *kubeKinds, namespace string) error {
	failed := 0

	for _, event := range []string{helmHookPreRollback, helmHookPostRollback} {
		for _, h := range helmEventHooks(rb.target.rel, event) {
			step := helmHookStep{hook: helmHook{
				Name: h.Name, Kind: h.Kind, Event: event, Weight: h.Weight,
				DeletePolicies: helmHookPolicies(h.DeletePolicies),
			}}

			obj, err := helmHookObject(ctx, kinds, h, namespace)
			if err == nil {
				step.obj = obj
				step.hook.Name, step.hook.Kind = cmp.Or(step.hook.Name, obj.Name), cmp.Or(step.hook.Kind, obj.Kind)
				err = dryRunHelmHook(ctx, k, step)
			}

			if err != nil {
				step.hook.Error = kubeError(err).Error()
				failed++
			}

			rb.plan.Hooks = append(rb.plan.Hooks, step.hook)

			if event == helmHookPreRollback {
				rb.preHooks = append(rb.preHooks, step)
			} else {
				rb.postHooks = append(rb.postHooks, step)
			}
		}
	}

	if failed > 0 {
		return fmt.Errorf("the API server refuses %d of the hooks (see each one)", failed)
	}

	return nil
}

// helmHookPolicies are a hook's delete policies, before-hook-creation when it sets none
// (Helm's hookSetDeletePolicy).
func helmHookPolicies(policies []string) []string {
	if len(policies) == 0 {
		return []string{helmHookBeforeCreation}
	}

	return slices.Clone(policies)
}

// helmHookError is a hook that failed while the rollback ran. KubeHelmRollback keeps it out
// of the Kubernetes call wrapper, which would reduce it to the API error inside it.
type helmHookError struct{ err error }

func (e *helmHookError) Error() string { return e.err.Error() }
func (e *helmHookError) Unwrap() error { return e.err }

// helmHookObject is the one object a hook's manifest renders.
func helmHookObject(ctx context.Context, kinds *kubeKinds, h helmReleaseHook, namespace string) (helmObject, error) {
	if strings.TrimSpace(h.Manifest) == "" {
		return helmObject{}, errors.New("the release stores no manifest for this hook")
	}

	objs, err := helmObjects(ctx, kinds, h.Manifest, namespace)
	if err != nil {
		return helmObject{}, err
	}

	if len(objs) != 1 {
		return helmObject{}, fmt.Errorf("the hook's manifest holds %d objects, not one", len(objs))
	}

	return objs[0], nil
}

// dryRunHelmHook checks the hook's creation and, for its delete policies, its deletion. An
// object of the same name already there is fine when before-hook-creation deletes it first.
func dryRunHelmHook(ctx context.Context, k *kubeClient, h helmHookStep) error {
	err := applyHelmStep(ctx, k, helmStep{action: helmChangeCreate, obj: h.obj}, true)
	if kubeCode(err) == http.StatusConflict && h.hasPolicy(helmHookBeforeCreation) {
		err = nil
	}

	if err != nil {
		return err
	}

	// Authorization comes before the lookup: a delete refused shows even when nothing exists.
	return applyHelmStep(ctx, k, helmStep{action: helmChangeDelete, obj: h.obj}, true)
}

// runHelmHooks runs one event's hooks in order; the error names the hook that failed.
func runHelmHooks(ctx context.Context, k *kubeClient, hooks []helmHookStep) error {
	for i, h := range hooks {
		if err := runHelmHook(ctx, k, h); err != nil {
			errs := []error{fmt.Errorf("%s failed: %w", h.label(), err), deleteHelmHook(ctx, k, h, helmHookFailed, false)}
			for _, passed := range hooks[:i] {
				errs = append(errs, deleteHelmHook(ctx, k, passed, helmHookSucceeded, false))
			}

			return &helmHookError{errors.Join(errs...)}
		}
	}

	for _, h := range slices.Backward(hooks) {
		if err := deleteHelmHook(ctx, k, h, helmHookSucceeded, false); err != nil {
			return &helmHookError{err}
		}
	}

	return nil
}

// runHelmHook replaces the hook's object when asked, creates it and waits for it.
func runHelmHook(ctx context.Context, k *kubeClient, h helmHookStep) error {
	if err := deleteHelmHook(ctx, k, h, helmHookBeforeCreation, true); err != nil {
		return err
	}

	if err := applyHelmStep(ctx, k, helmStep{action: helmChangeCreate, obj: h.obj}, false); err != nil {
		return fmt.Errorf("create: %w", kubeError(err))
	}

	if h.obj.Kind != "Job" && h.obj.Kind != "Pod" {
		return nil
	}

	return pollHelmHook(ctx, k, h.obj.Path, func(live map[string]any, gone bool) (bool, error) {
		if gone {
			return true, errors.New("deleted while it ran")
		}

		return helmHookDone(h.obj.Kind, live)
	})
}

// deleteHelmHook deletes the hook's object when it has policy (never a CRD, as Helm), and
// with wait until it is gone. One already gone is fine.
func deleteHelmHook(ctx context.Context, k *kubeClient, h helmHookStep, policy string, wait bool) error {
	if !h.hasPolicy(policy) || h.obj.Kind == "CustomResourceDefinition" || h.obj.Path == "" {
		return nil
	}

	q := url.Values{"propagationPolicy": {"Background"}}

	err := k.do(ctx, http.MethodDelete, h.obj.Path+"?"+q.Encode(), "application/json", nil, nil)
	if isNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("delete %s (%s): %w", h.label(), policy, kubeError(err))
	}

	if !wait {
		return nil
	}

	return pollHelmHook(ctx, k, h.obj.Path, func(_ map[string]any, gone bool) (bool, error) { return gone, nil })
}

// pollHelmHook reads path every helmHookPollInterval, helmHookTimeout at most, until check
// says done (gone: the object does not exist). A read refused ends it at once, other failed
// reads after followMaxErrors in a row, as pollFollow.
func pollHelmHook(ctx context.Context, k *kubeClient, path string, check func(live map[string]any, gone bool) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, helmHookTimeout)
	defer cancel()

	failures := 0

	for {
		var live map[string]any

		err := k.get(ctx, path, &live)

		switch {
		case err == nil || isNotFound(err):
			failures = 0

			if done, result := check(live, err != nil); done {
				return result
			}
		case ctx.Err() != nil:
		case followFatal(err):
			return kubeError(err)
		default:
			if failures++; failures >= followMaxErrors {
				return kubeError(err)
			}
		}

		if err := waitFollow(ctx, helmHookPollInterval); err != nil {
			return fmt.Errorf("not done after %s", helmHookTimeout)
		}
	}
}

// helmHookDone reads a Job's conditions or a Pod's phase: done, with the failure if any.
func helmHookDone(kind string, live map[string]any) (bool, error) {
	status := mapField(live, "status")

	if kind == "Pod" {
		phase, _ := status["phase"].(string)
		reason, _ := status["reason"].(string)
		message, _ := status["message"].(string)

		switch phase {
		case "Succeeded":
			return true, nil
		case "Failed":
			return true, helmHookFailure("Pod failed", reason, message)
		}

		return false, nil
	}

	conditions, _ := status["conditions"].([]any)

	for _, c := range conditions {
		cond, _ := c.(map[string]any)
		if cond["status"] != "True" {
			continue
		}

		reason, _ := cond["reason"].(string)
		message, _ := cond["message"].(string)

		switch cond["type"] {
		case "Complete":
			return true, nil
		case "Failed":
			return true, helmHookFailure("Job failed", reason, message)
		}
	}

	return false, nil
}

func helmHookFailure(what, reason, message string) error {
	for _, s := range []string{reason, message} {
		if s != "" {
			what += ": " + s
		}
	}

	return errors.New(what)
}
