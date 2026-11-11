package ichorgo

import (
	"context"
	"errors"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

// A followed operation is one the person started from the phone and then leaves (an Argo CD
// sync, a Flux reconcile): the apps hand it to their operation notification or Live
// Activity, which hears of it through a listener. The poll loop is kube.PollFollow.

// followFatal: retrying the read cannot help.
func followFatal(err error) bool {
	var signIn *errSignInRequired

	return errors.As(err, &signIn) || kube.FollowFatal(err)
}

// waitFollow waits d, or less when ctx ends first (its error is returned then).
func waitFollow(ctx context.Context, d time.Duration) error { return kube.WaitFollow(ctx, d) }
