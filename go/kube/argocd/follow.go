package argocd

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

// Following an Argo CD sync started from the phone: the Application is read every few
// seconds, as the app detail reads it (its operation, sync waves and per-resource results),
// until the operation ends. Only the operation the phone started counts: one that started
// before the follow (the last sync, or one a webhook ran) is ignored, so a follow that
// starts before the controller picked the sync up waits for the new one.

// Results of a followed sync: the operation's final phase, or SyncTerminated when it ended
// after a terminate from the run.
const (
	SyncSucceeded  = "Succeeded"
	SyncFailed     = "Failed"
	SyncError      = "Error"
	SyncTerminated = "Terminated"
	SyncPending    = "Pending" // the sync was requested, the controller has not started it
	SyncRunning    = "Running"
)

var (
	// FollowInterval is the pause between two reads of the followed app.
	FollowInterval = 3 * time.Second
	// FollowGrace is how many reads a follow waits for the sync it was started for to show up
	// before it reports whatever operation finished last.
	FollowGrace = 10
	// followSkew is how much older than the follow's start (the phone's clock against the
	// cluster's, second-rounded) an operation's start may be and still be the phone's.
	followSkew = 5 * time.Second
)

var ErrSyncNotStarted = &kube.APIError{Code: 409, Reason: "NotStarted", Message: "Argo CD has not started the sync"}

// SyncProgress is one step of a followed sync, as the apps' listener gets it.
type SyncProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	Wave    int    `json:"wave"`
	Waves   int    `json:"waves"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	At      int64  `json:"at"`
}

// With runs fn with a client of the cluster followed: a fresh one when the last one stopped
// working.
type With func(ctx context.Context, fn func(context.Context, kube.Client) error) error

// Follow reads the Application namespace/name every FollowInterval until the operation the
// phone started at since ends, emitting its progress; it returns the final phase. terminated
// tells whether the run asked the sync to stop; fatal, whether a read error ends the follow.
func Follow(ctx context.Context, with With, fatal func(error) bool, namespace, name string, since time.Time, terminated func() bool, emit func(SyncProgress)) (string, error) {
	f := &follow{since: since}

	var result string

	read := func(ctx context.Context) (bool, error) {
		var done bool

		err := with(ctx, func(ctx context.Context, k kube.Client) error {
			app, pending, err := readApp(ctx, k, namespace, name)
			if err != nil {
				return err
			}

			result, done, err = f.step(app.Operation, pending, terminated(), emit)

			return err
		})

		return done, err
	}

	err := kube.PollFollow(ctx, FollowInterval, read, fatal)

	return result, err
}

// follow is the state of a follow between two reads.
type follow struct {
	since time.Time
	reads int
	// stale is the start of the operation found finished while a new one was requested:
	// the last sync, not the phone's.
	stale int64
}

// mine tells whether op is the operation the phone started.
func (f *follow) mine(op *Operation) bool {
	return op != nil && op.StartedAt != f.stale && op.StartedAt >= f.since.Add(-followSkew).UnixMilli()
}

// step takes one read of the app: its operation op, and pending when a sync was requested
// and not picked up yet. It returns the result once done.
func (f *follow) step(op *Operation, pending, terminated bool, emit func(SyncProgress)) (string, bool, error) {
	f.reads++

	if pending && op != nil && op.Phase != SyncRunning && op.Phase != "Terminating" {
		f.stale = op.StartedAt
	}

	if !f.mine(op) {
		if f.reads <= FollowGrace {
			emit(SyncProgress{Phase: SyncPending})

			return "", false, nil
		}

		// The sync never showed: report what finished last, if anything did.
		if op == nil || pending {
			return "", true, ErrSyncNotStarted
		}
	}

	p := syncProgressOf(op)
	running := op.Phase == SyncRunning || op.Phase == "Terminating"

	if !running && terminated && p.Phase == SyncFailed {
		p.Phase = SyncTerminated
	}

	emit(p)

	if running {
		return "", false, nil
	}

	return p.Phase, true, nil
}

func syncProgressOf(op *Operation) SyncProgress {
	p := SyncProgress{Phase: op.Phase, Message: op.Message, Waves: len(op.Waves), Done: op.Done, Total: op.Total}

	if i := slices.Index(op.Waves, op.Wave); i >= 0 {
		p.Wave = i + 1
	}

	if strings.Contains(strings.ToLower(op.Message), "terminated") && op.Phase == SyncFailed {
		p.Phase = SyncTerminated
	}

	return p
}

// readApp reads one Application as the listing maps it, and whether a sync was requested
// that the controller has not started yet.
func readApp(ctx context.Context, k kube.Client, namespace, name string) (App, bool, error) {
	groups, err := kube.APIGroups(ctx, k)
	if err != nil {
		return App{}, false, err
	}

	version, ok := groups[Group]
	if !ok {
		return App{}, false, &kube.APIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	var o Object
	if err := k.Get(ctx, "/apis/"+Group+"/"+version+"/namespaces/"+url.PathEscape(namespace)+"/applications/"+url.PathEscape(name), &o); err != nil {
		return App{}, false, err
	}

	return MapApp(o, nil), len(o.Operation) > 0 && string(o.Operation) != "null", nil
}
