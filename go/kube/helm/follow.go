package helm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

// Waiting for a Helm release after a rollback, as `helm rollback --wait`: the Deployments,
// StatefulSets and DaemonSets of its deployed revision are read every few seconds until
// every pod runs their latest template and is ready (`kubectl rollout status`), one of them
// exceeded its progress deadline, or WaitTimeout passed. It is a run of its own, after
// the rollback: a slow rollout never leaves the release pending-rollback. Reads only:
// get/list on the release's Secrets, its apps/v1 workloads, their ReplicaSets and pods.

// Phases of a followed rollback (HelmRollbackListener.OnProgress), the last one its result.
const (
	WaitRunning   = "Running"
	WaitSucceeded = "Succeeded"
	WaitFailed    = "Failed"
	WaitTimedOut  = "Timeout"
)

// The pace of a followed rollback (variables for the tests).
var (
	// WaitInterval is the pause between two reads of the workloads.
	WaitInterval = 3 * time.Second
	// WaitTimeout bounds a wait, Helm's default --timeout.
	WaitTimeout = 5 * time.Minute
)

type Workload struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     int    `json:"ready"`
	Desired   int    `json:"desired"`
	Done      bool   `json:"done"`
	Failed    bool   `json:"failed"`
	// Manual: pods are replaced only when deleted (OnDelete, StatefulSet partition); counted
	// as done.
	Manual bool `json:"manual"`
}

type Progress struct {
	Phase     string     `json:"phase"`
	Done      int        `json:"done"`
	Total     int        `json:"total"`
	Workloads []Workload `json:"workloads"`
	At        int64      `json:"at"`
}

// Same: p and o differ only by their time.
func (p Progress) Same(o Progress) bool {
	return p.Phase == o.Phase && p.Done == o.Done && p.Total == o.Total && slices.Equal(p.Workloads, o.Workloads)
}

// With runs fn with a client of the cluster followed: a fresh one when the last one stopped
// working.
type With func(ctx context.Context, fn func(context.Context, Env) error) error

// Follow lists the workloads of the deployed revision, then reads them every WaitInterval
// until they are all done, one failed, or WaitTimeout passed (Timeout, no error). The wait
// starts once they are listed, as Helm's --timeout: the listing is one read, bounded by
// kube.FollowReadTimeout alone, so a slow API fails it rather than timing out the wait before
// it began. fatal tells that reading again cannot help.
func Follow(ctx context.Context, with With, fatal func(error) bool, namespace, name string, emit func(Progress)) (string, error) {
	var workloads []object

	readCtx, cancelRead := context.WithTimeout(ctx, kube.FollowReadTimeout)
	err := with(readCtx, func(ctx context.Context, k Env) error {
		var err error

		workloads, err = deployedWorkloads(ctx, k, namespace, name)

		return err
	})

	cancelRead()

	if err != nil {
		return "", err
	}

	if len(workloads) == 0 {
		emit(Progress{Phase: WaitSucceeded, Workloads: []Workload{}})

		return WaitSucceeded, nil
	}

	p := Progress{Phase: WaitRunning, Total: len(workloads), Workloads: []Workload{}}

	bounded, cancel := context.WithTimeout(ctx, WaitTimeout)
	defer cancel()

	read := func(ctx context.Context) (bool, error) {
		err := with(ctx, func(ctx context.Context, k Env) error {
			next, err := readWorkloads(ctx, k, workloads)
			if err != nil {
				return err
			}

			p = next
			emit(p)

			return nil
		})

		return p.Phase != WaitRunning, err
	}

	err = kube.PollFollow(bounded, WaitInterval, read, fatal)

	if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
		p.Phase = WaitTimedOut
		emit(p)

		return WaitTimedOut, nil
	}

	if err != nil {
		return "", err
	}

	return p.Phase, nil
}

// deployedWorkloads are the workloads of the latest revision of release namespace/name,
// which must be deployed.
func deployedWorkloads(ctx context.Context, k Env, namespace, name string) ([]object, error) {
	refs, err := SecretRefs(ctx, k.Client, namespace, name)
	if err != nil {
		return nil, err
	}

	refs = historyRefs(refs, name)
	if len(refs) == 0 {
		return nil, fmt.Errorf("no Helm release %s/%s", namespace, name)
	}

	stored, err := readStored(ctx, k.Client, refs[0])
	if err != nil {
		return nil, fmt.Errorf("revision %d: %w", refs[0].Revision, err)
	}

	if status := cmp.Or(stored.rel.Info.Status, refs[0].Status); status != StatusDeployed {
		return nil, fmt.Errorf("revision %d of %s is %s, not deployed", refs[0].Revision, name, status)
	}

	objs, err := objects(ctx, newKindResolver(k.Client), stored.rel.Manifest, cmp.Or(stored.rel.Namespace, namespace))
	if err != nil {
		return nil, err
	}

	return workloadObjects(objs), nil
}

// readWorkloads reads the rollout of each workload: Failed as soon as one failed,
// Succeeded once all are done.
func readWorkloads(ctx context.Context, k Env, workloads []object) (Progress, error) {
	p := Progress{Phase: WaitRunning, Total: len(workloads), Workloads: make([]Workload, 0, len(workloads))}

	for _, o := range workloads {
		st, err := k.Rollout(ctx, o.Kind, o.Namespace, o.Name)
		if err != nil {
			return Progress{}, fmt.Errorf("%s: %w", o.label(), err)
		}

		// As `kubectl rollout status`: until the controller observed the latest spec, its
		// status (a progress deadline exceeded included) is the previous rollout's.
		p.Workloads = append(p.Workloads, Workload{
			Kind: o.Kind, Namespace: o.Namespace, Name: o.Name,
			Ready: st.Ready, Desired: st.Desired,
			Done: st.Done && !st.Stale, Failed: st.Failed && !st.Stale, Manual: st.Manual,
		})
	}

	return WithWaitPhase(p), nil
}

// WithWaitPhase counts the workloads done and sets the phase from them.
func WithWaitPhase(p Progress) Progress {
	p.Done = 0
	failed := false

	for _, w := range p.Workloads {
		if w.Done || w.Manual {
			p.Done++
		}

		failed = failed || w.Failed
	}

	switch {
	case failed:
		p.Phase = WaitFailed
	case p.Done == p.Total:
		p.Phase = WaitSucceeded
	default:
		p.Phase = WaitRunning
	}

	return p
}
