package ichorgo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// snapshotRecorder is a SnapshotListener that keeps everything it is told.
type snapshotRecorder struct {
	progress chan int64
	done     chan snapshotOutcome
}

type snapshotOutcome struct {
	path, sum, err string
	size           int64
}

func newSnapshotRecorder() *snapshotRecorder {
	return &snapshotRecorder{progress: make(chan int64, 64), done: make(chan snapshotOutcome, 1)}
}

func (r *snapshotRecorder) OnProgress(n int64) {
	select {
	case r.progress <- n:
	default:
	}
}

func (r *snapshotRecorder) OnDone(path string, size int64, sum, errMessage string) {
	r.done <- snapshotOutcome{path: path, sum: sum, err: errMessage, size: size}
}

func (r *snapshotRecorder) wait(t *testing.T) snapshotOutcome {
	t.Helper()

	select {
	case o := <-r.done:
		return o
	case <-time.After(20 * time.Second):
		t.Fatal("OnDone not called")

		return snapshotOutcome{}
	}
}

// sendChunks streams data in 64 KiB messages.
func sendChunks(stream grpc.ServerStreamingServer[common.Data], data []byte) error {
	for len(data) > 0 {
		n := min(len(data), 64<<10)
		if err := stream.Send(&common.Data{Bytes: data[:n]}); err != nil {
			return err
		}

		data = data[n:]
	}

	return nil
}

func fakeEtcdNode(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, "192.0.2.11", "v1.11.0", machine.TypeControlPlane)

	return f, f.start(t, "192.0.2.11")
}

func TestStartEtcdSnapshotFake(t *testing.T) {
	data := bytes.Repeat([]byte("etcd-db-"), 300<<10) // 2.4 MB: a few progress reports

	f, cfg := fakeEtcdNode(t)
	f.snapshot = func(stream grpc.ServerStreamingServer[common.Data]) error { return sendChunks(stream, data) }

	dest := filepath.Join(t.TempDir(), "etcd.snapshot")
	r := newSnapshotRecorder()

	StartEtcdSnapshot(cfg, "fake", "192.0.2.11", dest, r)

	o := r.wait(t)
	if o.err != "" {
		t.Fatalf("snapshot failed: %s", o.err)
	}

	sum := sha256.Sum256(data)
	if o.path != dest || o.size != int64(len(data)) || o.sum != hex.EncodeToString(sum[:]) {
		t.Fatalf("OnDone = %+v", o)
	}

	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file = %d bytes, err %v", len(got), err)
	}

	if len(r.progress) < 2 {
		t.Errorf("progress reports = %d, want one per MiB and the last", len(r.progress))
	}

	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Error("the .part file is left behind")
	}
}

func TestStartEtcdSnapshotFakeFailures(t *testing.T) {
	tests := []struct {
		name     string
		node     string
		snapshot func(grpc.ServerStreamingServer[common.Data]) error
		want     string
	}{
		{
			name: "refused",
			node: "192.0.2.11",
			snapshot: func(grpc.ServerStreamingServer[common.Data]) error {
				return status.Error(codes.PermissionDenied, "not authorized")
			},
			want: "snapshot stream",
		},
		{
			name: "broken mid-stream",
			node: "192.0.2.11",
			snapshot: func(stream grpc.ServerStreamingServer[common.Data]) error {
				_ = sendChunks(stream, []byte("partial"))

				return status.Error(codes.Internal, "etcd went away")
			},
			want: "etcd went away",
		},
		{name: "unknown node", node: "192.0.2.99", want: "not part of this context"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cfg := fakeEtcdNode(t)
			f.snapshot = tt.snapshot

			dest := filepath.Join(t.TempDir(), "etcd.snapshot")
			r := newSnapshotRecorder()

			StartEtcdSnapshot(cfg, "fake", tt.node, dest, r)

			if o := r.wait(t); !strings.Contains(o.err, tt.want) {
				t.Fatalf("err = %q, want %q", o.err, tt.want)
			}

			for _, p := range []string{dest, dest + ".part"} {
				if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s exists after a failure", p)
				}
			}
		})
	}
}

func TestStartEtcdSnapshotFakeCancel(t *testing.T) {
	f, cfg := fakeEtcdNode(t)
	f.snapshot = func(stream grpc.ServerStreamingServer[common.Data]) error {
		if err := sendChunks(stream, bytes.Repeat([]byte{1}, 2<<20)); err != nil {
			return err
		}

		<-stream.Context().Done() // a slow node: the rest never comes

		return stream.Context().Err()
	}

	dest := filepath.Join(t.TempDir(), "etcd.snapshot")
	r := newSnapshotRecorder()

	run := StartEtcdSnapshot(cfg, "fake", "192.0.2.11", dest, r)

	select {
	case <-r.progress:
	case <-time.After(20 * time.Second):
		t.Fatal("no progress before the cancel")
	}

	run.Cancel()

	if o := r.wait(t); !strings.Contains(o.err, "cancel") {
		t.Fatalf("err = %q, want the cancel reported", o.err)
	}

	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Error("the partial file is left behind")
	}
}

func TestEtcdAlarmDisarmFake(t *testing.T) {
	f, cfg := fakeEtcdNode(t)

	if err := EtcdAlarmDisarm(cfg, "fake", "192.0.2.11"); err != nil {
		t.Fatal(err)
	}

	if got := f.called("EtcdAlarmDisarm"); len(got) != 1 || got[0] != "EtcdAlarmDisarm 192.0.2.11" {
		t.Fatalf("calls = %v", got)
	}

	f.disarmErr = status.Error(codes.PermissionDenied, "not authorized")

	if err := EtcdAlarmDisarm(cfg, "fake", "192.0.2.11"); err == nil {
		t.Fatal("a refused disarm must fail")
	}

	if err := EtcdAlarmDisarm(cfg, "fake", "192.0.2.12"); err == nil || !strings.Contains(err.Error(), "not part of this context") {
		t.Fatalf("err = %v, want the node refused", err)
	}
}
