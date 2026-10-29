package kube

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestPollFollow(t *testing.T) {
	flaky := errors.New("connection reset")
	gone := &APIError{Code: http.StatusNotFound}

	tests := []struct {
		name      string
		answers   []error // nil: not done yet; the read after the last answer is done
		wantErr   error
		wantReads int
	}{
		{"done after two reads", []error{nil}, nil, 2},
		{"a failure between successes is forgiven", []error{flaky, nil, flaky}, nil, 4},
		{"the object is gone", []error{nil, gone}, gone, 2},
		{"too many failures in a row", []error{flaky, flaky, flaky}, flaky, FollowMaxErrors},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := 0
			read := func(ctx context.Context) (bool, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("a read has no deadline")
				}

				reads++
				if reads > len(tt.answers) {
					return true, nil
				}

				return false, tt.answers[reads-1]
			}

			err := PollFollow(context.Background(), time.Millisecond, read, FollowFatal)
			if !errors.Is(err, tt.wantErr) || reads != tt.wantReads {
				t.Fatalf("err = %v after %d reads, want %v after %d", err, reads, tt.wantErr, tt.wantReads)
			}
		})
	}
}

func TestPollFollowStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	read := func(context.Context) (bool, error) {
		cancel()

		return false, nil
	}

	if err := PollFollow(ctx, time.Hour, read, FollowFatal); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestFollowFatalAndWait(t *testing.T) {
	for code, want := range map[int]bool{401: true, 403: true, 404: true, 409: false, 500: false} {
		if FollowFatal(&APIError{Code: code}) != want {
			t.Errorf("FollowFatal(%d) != %v", code, want)
		}
	}

	if FollowFatal(errors.New("dial tcp: i/o timeout")) {
		t.Error("a transport error is fatal")
	}

	if err := WaitFollow(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := WaitFollow(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
