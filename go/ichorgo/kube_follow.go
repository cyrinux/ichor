package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// A followed operation is one the person started from the phone and then leaves (an Argo CD
// sync, a Flux reconcile): the apps hand it to their operation notification or Live
// Activity, which hears of it through a listener. Its object is read again every few seconds
// until the operation ends; one read that fails does not end the follow, a few in a row do.

const (
	// followReadTimeout bounds one read of a followed object.
	followReadTimeout = 30 * time.Second
	// followMaxErrors failed reads in a row end a follow.
	followMaxErrors = 3
)

// pollFollow calls step with target's client at once, then every interval, until step says
// the operation is done, ctx ends (its error is returned) or the reads fail: at once when
// the object is gone or the credentials are refused, after followMaxErrors failures in a
// row otherwise.
func pollFollow(ctx context.Context, target kubeTarget, interval time.Duration, step func(context.Context, *kubeClient) (bool, error)) error {
	failures := 0

	for {
		readCtx, cancel := context.WithTimeout(ctx, followReadTimeout)
		done, err := withKubeContext(readCtx, target, step)
		cancel()

		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err == nil && done:
			return nil
		case err == nil:
			failures = 0
		case followFatal(err):
			return err
		default:
			if failures++; failures >= followMaxErrors {
				return err
			}
		}

		if err := waitFollow(ctx, interval); err != nil {
			return err
		}
	}
}

// followFatal: retrying the read cannot help.
func followFatal(err error) bool {
	var signIn *errSignInRequired
	if errors.As(err, &signIn) {
		return true
	}

	switch kubeCode(err) {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	}

	return false
}

// waitFollow waits d, or less when ctx ends first (its error is returned then).
func waitFollow(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
