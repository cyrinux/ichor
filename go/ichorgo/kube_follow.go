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

const (
	// followReadTimeout bounds one read of a followed object.
	followReadTimeout = kube.FollowReadTimeout
	// followMaxErrors failed reads in a row end a follow.
	followMaxErrors = kube.FollowMaxErrors
)

// pollFollow calls step with target's client at once, then every interval, until step says
// the operation is done, ctx ends (its error is returned) or the reads fail: at once when
// the object is gone or the credentials are refused, after followMaxErrors failures in a
// row otherwise.
func pollFollow(ctx context.Context, target kubeTarget, interval time.Duration, step func(context.Context, *kubeClient) (bool, error)) error {
	read := func(readCtx context.Context) (bool, error) { return withKubeContext(readCtx, target, step) }

	return kube.PollFollow(ctx, interval, read, followFatal)
}

// followFatal: retrying the read cannot help.
func followFatal(err error) bool {
	var signIn *errSignInRequired

	return errors.As(err, &signIn) || kube.FollowFatal(err)
}

// waitFollow waits d, or less when ctx ends first (its error is returned then).
func waitFollow(ctx context.Context, d time.Duration) error { return kube.WaitFollow(ctx, d) }
