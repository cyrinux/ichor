package flux

import (
	"context"
	"errors"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

// Following a Flux reconcile started from the phone. Flux has no operation object: the
// reconcile request is the reconcile.fluxcd.io/requestedAt annotation, which the controller
// copies to status.lastHandledReconcileAt once it handled it; the Ready condition then says
// how it went. The object is read every few seconds until then, at most FollowTimeout:
// a reconcile still running by then is reported as such, not as a failure.

// Phases of a followed reconcile, and the result of one that outlived the follow.
const (
	FollowReconciling = "reconciling"
	FollowReady       = "ready"
	FollowFailed      = "failed"
	FollowStill       = "still reconciling"
)

var (
	// FollowInterval is the pause between two reads of the followed object.
	FollowInterval = 3 * time.Second
	// FollowTimeout bounds a follow.
	FollowTimeout = 10 * time.Minute
)

// Progress is where a followed reconcile stands.
type Progress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

// followObject is what a follow reads of a Flux object of any kind.
type followObject struct {
	Metadata struct {
		Meta
		Generation int64 `json:"generation"`
	} `json:"metadata"`
	Spec struct {
		Suspend bool `json:"suspend"`
	} `json:"spec"`
	Status struct {
		CommonStatus
		ObservedGeneration int64 `json:"observedGeneration"`
	} `json:"status"`
}

// With runs fn with a client of the cluster followed: a fresh one when the last one stopped
// working.
type With func(ctx context.Context, fn func(context.Context, kube.Client) error) error

// Follow reads the object every FollowInterval until the reconcile was handled, or
// FollowTimeout passed (FollowStill, no error). Reads only. fatal tells that reading again
// cannot help. emit hears where it stands at every read.
func Follow(ctx context.Context, with With, fatal func(error) bool, kind, namespace, name string, emit func(Progress)) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, FollowTimeout)
	defer cancel()

	var result string

	err := kube.PollFollow(bounded, FollowInterval, func(readCtx context.Context) (bool, error) {
		done := false

		err := with(readCtx, func(ctx context.Context, k kube.Client) error {
			var err error

			done, result, err = followRead(ctx, k, kind, namespace, name, emit)

			return err
		})

		return done, err
	}, fatal)

	if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
		return FollowStill, nil
	}

	return result, err
}

// followRead reads the object once: whether the reconcile ended, and its result if so.
func followRead(ctx context.Context, k kube.Client, kind, namespace, name string, emit func(Progress)) (bool, string, error) {
	groups, err := kube.APIGroups(ctx, k)
	if err != nil {
		return false, "", err
	}

	path, err := Path(groups, kind, namespace, name)
	if err != nil {
		return false, "", err
	}

	var obj followObject
	if err := k.Get(ctx, path, &obj); err != nil {
		return false, "", err
	}

	p := progressOf(obj)
	emit(p)

	if p.Phase == FollowReconciling {
		return false, "", nil
	}

	return true, p.Phase, nil
}

// progressOf is where the reconcile of obj stands. The request is handled once
// the controller echoed the requestedAt annotation back; without one (nothing to compare),
// once the controller observed the object's generation. Ready Unknown is still running.
func progressOf(obj followObject) Progress {
	ready := obj.Status.Conditions.Get("Ready")
	msg := ready.Message

	if obj.Spec.Suspend {
		return Progress{Phase: FollowFailed, Message: "suspended: resume it first"}
	}

	req := obj.Metadata.Annotations[requestedAt]

	handled := obj.Status.LastHandledReconcileAt == req
	if req == "" {
		handled = obj.Status.ObservedGeneration >= obj.Metadata.Generation
	}

	switch {
	case !handled || ready.Status == "Unknown" || ready.Status == "":
		if r := obj.Status.Conditions.Get("Reconciling"); r.Status == "True" && r.Message != "" {
			msg = r.Message
		}

		return Progress{Phase: FollowReconciling, Message: msg}
	case ready.Status == "True":
		return Progress{Phase: FollowReady, Message: msg}
	default:
		return Progress{Phase: FollowFailed, Message: msg}
	}
}
