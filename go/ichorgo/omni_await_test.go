package ichorgo

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Omni's proxy cuts the long wait for the confirmation: the wait goes on.
func TestOmniConfirmationWaitSurvivesACutConnection(t *testing.T) {
	omniAwaitRetry = time.Millisecond
	t.Cleanup(func() { omniAwaitRetry = time.Second })

	calls := 0
	err := awaitOmniConfirmation(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return status.Error(codes.Unavailable, "error reading from server: EOF")
		}

		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err = %v after %d calls, want the third to confirm", err, calls)
	}
}

func TestOmniConfirmationWaitStopsOnARefusal(t *testing.T) {
	calls := 0
	err := awaitOmniConfirmation(context.Background(), func(context.Context) error {
		calls++

		return status.Error(codes.PermissionDenied, "key revoked")
	})
	if calls != 1 || !strings.Contains(err.Error(), "key revoked") {
		t.Fatalf("err = %v after %d calls, want the refusal at once", err, calls)
	}
}

func TestOmniConfirmationWaitEndsInTime(t *testing.T) {
	omniAwaitRetry = time.Millisecond
	t.Cleanup(func() { omniAwaitRetry = time.Second })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := awaitOmniConfirmation(ctx, func(ctx context.Context) error {
		return status.Error(codes.Unavailable, "EOF")
	})
	if err == nil || !strings.Contains(err.Error(), "not confirmed in Omni in time") {
		t.Fatalf("err = %v, want the timeout explained", err)
	}
}
