package kube

import (
	"context"
	"net/http"
	"time"
)

// A followed operation is one the person started from the phone and then leaves (an Argo CD
// sync, a Flux reconcile): its object is read again every few seconds until the operation
// ends; one read that fails does not end the follow, a few in a row do.

const (
	// FollowReadTimeout bounds one read of a followed object.
	FollowReadTimeout = 30 * time.Second
	// FollowMaxErrors failed reads in a row end a follow.
	FollowMaxErrors = 3
)

// PollFollow calls read at once, then every interval, each bounded by FollowReadTimeout,
// until read says the operation is done, ctx ends (its error is returned) or the reads fail:
// at once when fatal says retrying cannot help, after FollowMaxErrors failures in a row
// otherwise.
func PollFollow(ctx context.Context, interval time.Duration, read func(context.Context) (bool, error), fatal func(error) bool) error {
	failures := 0

	for {
		readCtx, cancel := context.WithTimeout(ctx, FollowReadTimeout)
		done, err := read(readCtx)
		cancel()

		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err == nil && done:
			return nil
		case err == nil:
			failures = 0
		case fatal(err):
			return err
		default:
			if failures++; failures >= FollowMaxErrors {
				return err
			}
		}

		if err := WaitFollow(ctx, interval); err != nil {
			return err
		}
	}
}

// FollowFatal reports an API answer that reading again cannot change: the object is gone or
// the credentials are refused.
func FollowFatal(err error) bool {
	switch Code(err) {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	}

	return false
}

// WaitFollow waits d, or less when ctx ends first (its error is returned then).
func WaitFollow(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
