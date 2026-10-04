package ichorgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

const snapshotTimeout = 30 * time.Minute

// SnapshotListener receives etcd snapshot progress (implemented in Kotlin/Swift).
type SnapshotListener interface {
	OnProgress(bytes int64)
	// OnDone is called exactly once; errMessage is empty on success.
	OnDone(path string, size int64, sha256 string, errMessage string)
}

// SnapshotRun is a handle on a running snapshot.
type SnapshotRun struct {
	cancel context.CancelFunc
}

// Cancel aborts the snapshot; the partial file is removed and OnDone reports the error.
func (r *SnapshotRun) Cancel() {
	r.cancel()
}

// StartEtcdSnapshot streams an etcd snapshot from node into destPath, like
// `talosctl -n NODE etcd snapshot FILE` (os:operator, os:etcd:backup or os:admin).
// The file is written to destPath.part and renamed when complete, so destPath only ever
// holds a whole snapshot. It contains every Kubernetes Secret: treat it accordingly.
func StartEtcdSnapshot(configYAML, contextName, node, destPath string, listener SnapshotListener) *SnapshotRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedSnapshotListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", 0, "", msg) })

		size, sum, err := runSnapshot(ctx, configYAML, contextName, node, destPath, listener)
		if err != nil {
			listener.OnDone("", 0, "", err.Error())

			return
		}

		listener.OnDone(destPath, size, sum, "")
	}()

	return &SnapshotRun{cancel: cancel}
}

func runSnapshot(ctx context.Context, configYAML, contextName, node, destPath string, listener SnapshotListener) (int64, string, error) {
	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		return 0, "", err
	}

	defer release()

	if err := validatePowerTarget(s.context, node); err != nil {
		return 0, "", err
	}

	r, err := s.client.EtcdSnapshot(client.WithNode(ctx, node), &machineapi.EtcdSnapshotRequest{})
	if err != nil {
		return 0, "", errors.New(s.friendly(node, err))
	}

	defer r.Close() //nolint:errcheck

	size, sum, err := writeSnapshot(r, destPath, listener.OnProgress)
	if err != nil && ctx.Err() != nil {
		return 0, "", errors.New(friendlyError(ctx.Err()))
	}

	return size, sum, err
}

// writeSnapshot copies r to destPath atomically (via destPath.part), reporting progress
// about every MiB, and returns the size and SHA-256 of what was written.
func writeSnapshot(r io.Reader, destPath string, progress func(int64)) (int64, string, error) {
	if !filepath.IsAbs(destPath) {
		return 0, "", fmt.Errorf("destination %q is not an absolute path", destPath)
	}

	part := destPath + ".part"

	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, "", err
	}

	hash := sha256.New()
	buf := make([]byte, 256*1024)

	var written, reported int64

	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return abort(f, part, err)
			}

			hash.Write(buf[:n])
			written += int64(n)

			if written-reported >= 1<<20 {
				reported = written
				progress(written)
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}

		if readErr != nil {
			return abort(f, part, fmt.Errorf("snapshot stream: %w", readErr))
		}
	}

	if err := f.Sync(); err != nil {
		return abort(f, part, err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(part) //nolint:errcheck

		return 0, "", err
	}

	if err := os.Rename(part, destPath); err != nil {
		_ = os.Remove(part) //nolint:errcheck

		return 0, "", err
	}

	progress(written)

	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func abort(f *os.File, part string, err error) (int64, string, error) {
	_ = f.Close()       //nolint:errcheck
	_ = os.Remove(part) //nolint:errcheck

	return 0, "", err
}

// EtcdAlarmDisarm clears etcd alarms (e.g. NOSPACE after freeing space), like
// `talosctl etcd alarm disarm` (os:operator or os:admin). Alarms are cluster-wide;
// the call goes through node.
func EtcdAlarmDisarm(configYAML, contextName, node string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return nodeAction(configYAML, contextName, node, callTimeout, func(ctx context.Context, c *client.Client) error {
		_, err := c.EtcdAlarmDisarm(ctx)

		return err
	})
}
