package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Deleting what a Scaleway create made. A running server goes with the terminate action, which
// takes its local volumes; one that never ran is deleted, which keeps them. Block Storage volumes
// and the flexible IP outlive the server either way and are deleted after it. Every step is
// remembered, so the cleanup's next try only does what is left.

// scwLeftover names what is still there (and billed) after a delete: the cleanup prints it as is.
type scwLeftover struct {
	zone  string
	left  []string
	cause error
}

func (e *scwLeftover) Error() string {
	reason := friendlyError(e.cause)
	if errors.Is(e.cause, context.DeadlineExceeded) {
		reason = "Scaleway had not finished in time: check that they are gone"
	}

	return "still there in " + e.zone + ": " + strings.Join(e.left, ", ") + " (" + reason + ")"
}

func (e *scwLeftover) Unwrap() error { return e.cause }

func (*scwLeftover) unfinishedDelete() {}

func (h *scwCreate) deleteMachine(ctx context.Context) error {
	h.lookup(ctx)

	var (
		left  []string
		cause error
	)

	note := func(what string, err error) {
		left = append(left, what)
		if cause == nil {
			cause = err
		}
	}

	if h.serverID != "" && !h.serverGone {
		if err := h.deleteServer(ctx); err != nil {
			note("Instance "+h.serverID, err)
		}
	}

	if h.serverGone {
		for _, v := range h.volumes {
			if v.gone || (v.kind != scwVolumeBlock && !h.keptVolumes) {
				continue
			}

			if err := h.deleteVolume(ctx, v); err != nil {
				note("volume "+v.id, err)
			}
		}
	}

	if h.ipID != "" && !h.ipGone {
		if err := h.deleteGone(ctx, h.client.path("/ips/"+url.PathEscape(h.ipID))); err != nil {
			note("flexible IP "+h.ipAddress+" ("+h.ipID+")", err)
		} else {
			h.ipGone = true
		}
	}

	if len(left) > 0 {
		return &scwLeftover{zone: h.client.zone, left: left, cause: cause}
	}

	return nil
}

// deleteServer terminates a running server or deletes a stopped one, and waits until it is gone.
func (h *scwCreate) deleteServer(ctx context.Context) error {
	terminated := false

	for {
		var resp struct {
			Server scwServer `json:"server"`
		}

		err := h.client.do(ctx, http.MethodGet, h.serverPath(""), nil, &resp)

		switch {
		case scwStatus(err) == http.StatusNotFound:
			h.serverGone = true

			return nil
		case err != nil && !h.transient(err):
			return err
		case err != nil:
		case resp.Server.State == scwStateLocked:
			return errors.New("scaleway locked it")
		case resp.Server.State == scwStateStopped || resp.Server.State == scwStateStoppedPlace:
			if err := h.client.do(ctx, http.MethodDelete, h.serverPath(""), nil, nil); err != nil && scwStatus(err) != http.StatusNotFound {
				return err
			}

			h.serverGone, h.keptVolumes = true, !terminated

			return nil
		case resp.Server.State == scwStateRunning && !terminated:
			if err := h.client.do(ctx, http.MethodPost, h.serverPath("/action"), map[string]string{"action": "terminate"}, nil); err != nil {
				return err
			}

			terminated = true
		}

		if err := scwWait(ctx, h.poll); err != nil {
			return err
		}
	}
}

// deleteVolume deletes a volume, waiting while it is still attached to the server going away.
func (h *scwCreate) deleteVolume(ctx context.Context, v *scwVolume) error {
	path := h.client.path("/volumes/" + url.PathEscape(v.id))
	if v.kind == scwVolumeBlock {
		path = "/block/v1alpha1/zones/" + h.client.zone + "/volumes/" + url.PathEscape(v.id)
	}

	if err := h.deleteGone(ctx, path); err != nil {
		return err
	}

	v.gone = true

	return nil
}

// deleteGone deletes path; gone already is deleted, and a conflict (still attached) or a provider
// hiccup is tried again until ctx ends.
func (h *scwCreate) deleteGone(ctx context.Context, path string) error {
	for {
		err := h.client.do(ctx, http.MethodDelete, path, nil, nil)

		status := scwStatus(err)

		switch {
		case err == nil || status == http.StatusNotFound:
			return nil
		case status != http.StatusConflict && status != http.StatusPreconditionFailed && !h.transient(err):
			return err
		}

		if werr := scwWait(ctx, h.poll); werr != nil {
			return err
		}
	}
}

func scwWait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
