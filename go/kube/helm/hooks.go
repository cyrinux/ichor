package helm

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

	"github.com/cyrinux/ichor/go/kube"
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
	HookPreRollback  = "pre-rollback"
	HookPostRollback = "post-rollback"

	HookBeforeCreation = "before-hook-creation"
	HookSucceeded      = "hook-succeeded"
	HookFailed         = "hook-failed"

	// hookTimeout bounds the wait for one hook (Helm's --timeout default is 5 minutes).
	hookTimeout = 5 * time.Minute

	kindCRD = "CustomResourceDefinition"
)

var (
	// errHookTimeout ends a wait that reached hookTimeout.
	errHookTimeout = fmt.Errorf("not done after %s", hookTimeout)
	// errRollbackTimeout ends a rollback's run that reached its deadline.
	errRollbackTimeout = errors.New("the rollback ran out of time")
)

// HookPollInterval is how often a hook object is read while waited for (a variable for
// the tests).
var HookPollInterval = 2 * time.Second

// Hook is one hook the rollback runs, in the plan. DeletePolicies are the effective ones
// (before-hook-creation when the chart sets none); Error is why the API server refused its
// dry run (a refused hook blocks the rollback).
type Hook struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Event          string   `json:"event"` // "pre-rollback" or "post-rollback"
	Weight         int      `json:"weight"`
	DeletePolicies []string `json:"deletePolicies"`
	Error          string   `json:"error"`
}

// hookStep is a hook ready to run: its object as the target revision rendered it.
type hookStep struct {
	hook Hook
	obj  object
}

func (h hookStep) label() string {
	return h.hook.Event + " hook " + h.hook.Kind + " " + h.hook.Name
}

func (h hookStep) hasPolicy(policy string) bool {
	return slices.Contains(h.hook.DeletePolicies, policy)
}

// eventHooks are target's hooks firing on event, in Helm's order (hookByWeight, stable).
func eventHooks(target Release, event string) []ReleaseHook {
	out := []ReleaseHook{}

	for _, h := range target.Hooks {
		if slices.Contains(h.Events, event) {
			out = append(out, h)
		}
	}

	slices.SortStableFunc(out, func(a, b ReleaseHook) int {
		return cmp.Or(cmp.Compare(a.Weight, b.Weight), strings.Compare(a.Name, b.Name))
	})

	return out
}

// planHooks resolves and dry-runs the target revision's rollback hooks into the plan and
// the steps the run takes; the error says how many the API server refuses.
func (rb *rollback) planHooks(ctx context.Context, k Env, kinds *kindResolver, namespace string) error {
	failed := 0

	for _, event := range []string{HookPreRollback, HookPostRollback} {
		for _, h := range eventHooks(rb.target.rel, event) {
			step := hookStep{hook: Hook{
				Name: h.Name, Kind: h.Kind, Event: event, Weight: h.Weight,
				DeletePolicies: hookPolicies(h.DeletePolicies),
			}}

			obj, err := hookObject(ctx, kinds, h, namespace)
			if err == nil {
				step.obj = obj
				step.hook.Name, step.hook.Kind = cmp.Or(step.hook.Name, obj.Name), cmp.Or(step.hook.Kind, obj.Kind)
				err = dryRunHook(ctx, k, step)
			}

			if err != nil {
				step.hook.Error = k.apiError(err).Error()
				failed++
			}

			rb.plan.Hooks = append(rb.plan.Hooks, step.hook)

			if event == HookPreRollback {
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

// hookPolicies are a hook's delete policies, before-hook-creation when it sets none
// (Helm's hookSetDeletePolicy).
func hookPolicies(policies []string) []string {
	if len(policies) == 0 {
		return []string{HookBeforeCreation}
	}

	return slices.Clone(policies)
}

// hookError is a hook that failed while the rollback ran. Rollback keeps it out
// of the Kubernetes call wrapper, which would reduce it to the API error inside it.
type hookError struct{ err error }

func (e *hookError) Error() string { return e.err.Error() }
func (e *hookError) Unwrap() error { return e.err }

// hookObject is the one object a hook's manifest renders.
func hookObject(ctx context.Context, kinds *kindResolver, h ReleaseHook, namespace string) (object, error) {
	if strings.TrimSpace(h.Manifest) == "" {
		return object{}, errors.New("the release stores no manifest for this hook")
	}

	objs, err := objects(ctx, kinds, h.Manifest, namespace)
	if err != nil {
		return object{}, err
	}

	if len(objs) != 1 {
		return object{}, fmt.Errorf("the hook's manifest holds %d objects, not one", len(objs))
	}

	return objs[0], nil
}

// dryRunHook checks the hook's creation and, for its delete policies, its deletion. An
// object of the same name already there is fine when before-hook-creation deletes it first,
// which it never does for a CustomResourceDefinition (the run's create would conflict).
func dryRunHook(ctx context.Context, k Env, h hookStep) error {
	err := applyStep(ctx, k, rollbackStep{action: ChangeCreate, obj: h.obj}, true)
	if kube.Code(err) == http.StatusConflict && h.hasPolicy(HookBeforeCreation) && h.obj.Kind != kindCRD {
		err = nil
	}

	if err != nil {
		return err
	}

	// Authorization comes before the lookup: a delete refused shows even when nothing exists.
	return applyStep(ctx, k, rollbackStep{action: ChangeDelete, obj: h.obj}, true)
}

// runHooks runs one event's hooks in order; the error names the hook that failed.
func runHooks(ctx context.Context, k Env, hooks []hookStep) error {
	for i, h := range hooks {
		if err := runHook(ctx, k, h); err != nil {
			errs := []error{fmt.Errorf("%s failed: %w", h.label(), err), deleteHook(ctx, k, h, HookFailed, false)}
			for _, passed := range hooks[:i] {
				errs = append(errs, deleteHook(ctx, k, passed, HookSucceeded, false))
			}

			return &hookError{errors.Join(errs...)}
		}
	}

	for _, h := range slices.Backward(hooks) {
		if err := deleteHook(ctx, k, h, HookSucceeded, false); err != nil {
			return &hookError{err}
		}
	}

	return nil
}

// runHook replaces the hook's object when asked, creates it and waits for it.
func runHook(ctx context.Context, k Env, h hookStep) error {
	if err := deleteHook(ctx, k, h, HookBeforeCreation, true); err != nil {
		return err
	}

	if err := applyStep(ctx, k, rollbackStep{action: ChangeCreate, obj: h.obj}, false); err != nil {
		return fmt.Errorf("create: %w", k.apiError(err))
	}

	if h.obj.Kind != "Job" && h.obj.Kind != "Pod" {
		return nil
	}

	// Gone counts as done, as Helm's watcher takes watch.Deleted: a Job with
	// ttlSecondsAfterFinished: 0 may be deleted before (or as soon as) it is first read.
	return pollHook(ctx, k, h.obj.Path, func(live map[string]any, gone bool) (bool, error) {
		if gone {
			return true, nil
		}

		return hookDone(h.obj.Kind, live)
	})
}

// deleteHook deletes the hook's object when it has policy (never a CRD, as Helm), and
// with wait until it is gone. One already gone is fine.
func deleteHook(ctx context.Context, k Env, h hookStep, policy string, wait bool) error {
	if !h.hasPolicy(policy) || h.obj.Kind == kindCRD || h.obj.Path == "" {
		return nil
	}

	q := url.Values{"propagationPolicy": {"Background"}}

	err := k.Do(ctx, http.MethodDelete, h.obj.Path+"?"+q.Encode(), "application/json", nil, nil)
	if kube.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("delete %s (%s): %w", h.label(), policy, k.apiError(err))
	}

	if !wait {
		return nil
	}

	return pollHook(ctx, k, h.obj.Path, func(_ map[string]any, gone bool) (bool, error) { return gone, nil })
}

// pollHook reads path every HookPollInterval, hookTimeout at most, until check
// says done (gone: the object does not exist). A read refused ends it at once, other failed
// reads after kube.FollowMaxErrors in a row, as kube.PollFollow.
func pollHook(ctx context.Context, k Env, path string, check func(live map[string]any, gone bool) (bool, error)) error {
	ctx, cancel := context.WithTimeoutCause(ctx, hookTimeout, errHookTimeout)
	defer cancel()

	failures := 0

	for {
		var live map[string]any

		err := k.Get(ctx, path, &live)

		switch {
		case err == nil || kube.IsNotFound(err):
			failures = 0

			if done, result := check(live, err != nil); done {
				return result
			}
		case ctx.Err() != nil:
		case k.fatal(err):
			return k.apiError(err)
		default:
			if failures++; failures >= kube.FollowMaxErrors {
				return k.apiError(err)
			}
		}

		if err := kube.WaitFollow(ctx, HookPollInterval); err != nil {
			return hookWaitError(ctx)
		}
	}
}

// hookWaitError says which end stopped a wait: the hook's own limit, the rollback's
// deadline or a cancellation.
func hookWaitError(ctx context.Context) error {
	cause := context.Cause(ctx)

	switch {
	case errors.Is(cause, errHookTimeout), errors.Is(cause, errRollbackTimeout):
		return cause
	case errors.Is(cause, context.DeadlineExceeded):
		return errRollbackTimeout
	case cause != nil:
		return fmt.Errorf("stopped while waiting: %w", cause)
	}

	return fmt.Errorf("stopped while waiting: %w", ctx.Err())
}

// hookDone reads a Job's conditions or a Pod's phase: done, with the failure if any.
func hookDone(kind string, live map[string]any) (bool, error) {
	status := mapField(live, "status")

	if kind == "Pod" {
		phase, _ := status["phase"].(string)
		reason, _ := status["reason"].(string)
		message, _ := status["message"].(string)

		switch phase {
		case "Succeeded":
			return true, nil
		case "Failed":
			return true, hookFailure("Pod failed", reason, message)
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
			return true, hookFailure("Job failed", reason, message)
		}
	}

	return false, nil
}

func hookFailure(what, reason, message string) error {
	for _, s := range []string{reason, message} {
		if s != "" {
			what += ": " + s
		}
	}

	return errors.New(what)
}
