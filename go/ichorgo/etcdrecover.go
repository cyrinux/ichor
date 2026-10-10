package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

// etcd recovery from a snapshot on the phone, like `talosctl -n NODE bootstrap
// --recover-from=FILE`: the snapshot (decrypted on the fly, never written in clear) is
// uploaded to one control plane, which bootstraps a new one-member etcd from it. It is for a
// cluster that lost etcd: it refuses while a member still answers.

const (
	// etcdRecoverTimeout bounds the whole run: the upload, the bootstrap and the wait.
	etcdRecoverTimeout = 30 * time.Minute
	// etcdRecoverWait bounds the wait for the new member to answer.
	etcdRecoverWait = 5 * time.Minute
	// etcdRecoverPoll is how often the new member is asked.
	etcdRecoverPoll = 5 * time.Second
)

// Phases of a recovery, in order.
const (
	recoverDecrypting    = "decrypting"
	recoverUploading     = "uploading"
	recoverBootstrapping = "bootstrapping"
	recoverWaiting       = "waiting"
	recoverDone          = "done"
)

// EtcdRecoverListener follows an etcd recovery (implemented in Kotlin/Swift).
type EtcdRecoverListener interface {
	// OnProgress gets {"phase","message","at","bytes","total"} on every change: bytes is how
	// much of the file was uploaded, total its size.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty on success.
	OnDone(errMessage string)
}

// EtcdRecoverRun is a handle on a running recovery.
type EtcdRecoverRun struct {
	cancel context.CancelFunc
}

// Cancel stops the recovery; once the bootstrap is requested it cannot be undone.
func (r *EtcdRecoverRun) Cancel() { r.cancel() }

type etcdRecoverProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      int64  `json:"at"`
	Bytes   int64  `json:"bytes"`
	Total   int64  `json:"total"`
}

// recoverRequest is what a recovery was asked to do.
type recoverRequest struct {
	node          string
	path          string
	identity      string
	passphrase    string
	skipHashCheck bool
}

// StartEtcdRecover recovers etcd on node (one control plane) from the snapshot at
// snapshotPath (os:admin). An age-encrypted file is opened with identity (an age secret key)
// or passphrase and decrypted while it streams; a clear file needs neither. skipHashCheck
// accepts a database copied from a member's data directory (it has no hash). The run refuses
// while any etcd member still answers: recovering a live cluster would split it.
func StartEtcdRecover(
	configYAML, contextName, node, snapshotPath, identity, passphrase string, skipHashCheck bool,
	listener EtcdRecoverListener,
) *EtcdRecoverRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedEtcdRecoverListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), etcdRecoverTimeout)

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		req := recoverRequest{node: strings.TrimSpace(node), path: snapshotPath, identity: identity, passphrase: passphrase, skipHashCheck: skipHashCheck}
		digest := ""

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "etcd-recover", Node: req.node, Params: fmt.Sprintf("sha256=%s skipHashCheck=%t", digest, skipHashCheck)}
		}, func() error {
			return runEtcdRecover(ctx, configYAML, contextName, req, &digest, func(p etcdRecoverProgress) { emitJSON(p, listener.OnProgress) })
		})

		listener.OnDone(errText(err))
	}()

	return &EtcdRecoverRun{cancel: cancel}
}

func runEtcdRecover(ctx context.Context, configYAML, contextName string, req recoverRequest, digest *string, emit func(etcdRecoverProgress)) error {
	if isDemoContext(configYAML, contextName) {
		return errDemoUnavailable
	}

	if strings.Contains(req.node, ",") {
		return errors.New("recover etcd on one control plane only: the others join it afterwards")
	}

	info, err := readSnapshotInfo(req.path)
	if err != nil {
		return err
	}

	*digest = info.Sha256

	decrypt, err := snapshotDecryptor(req.identity, req.passphrase)
	if err != nil {
		return err
	}

	s, release, err := acquireSession(configYAML, contextName)
	if err != nil {
		return err
	}

	defer release()

	if err := recoverGate(ctx, s, req.node); err != nil {
		return err
	}

	report := func(phase, msg string, bytes int64) {
		emit(etcdRecoverProgress{Phase: phase, Message: msg, At: time.Now().UnixMilli(), Bytes: bytes, Total: info.Size})
	}

	if err := uploadSnapshot(ctx, s, req, decrypt, info, report); err != nil {
		return err
	}

	report(recoverBootstrapping, "bootstrapping etcd from the snapshot", info.Size)

	nodeCtx := client.WithNode(ctx, req.node)

	if err := s.client.Bootstrap(nodeCtx, &machineapi.BootstrapRequest{RecoverEtcd: true, RecoverSkipHashCheck: req.skipHashCheck}); err != nil {
		return fmt.Errorf("the snapshot was uploaded, but the bootstrap failed: %s", s.friendlyErr(req.node, err))
	}

	return waitRecovered(ctx, s, req.node, report, info.Size)
}

// recoverGate checks node is a control plane of the context and that etcd is lost: a member
// that still answers means the cluster lives, and a recovery would split it.
func recoverGate(ctx context.Context, s *session, node string) error {
	if err := validatePowerTarget(s.context, node); err != nil {
		return err
	}

	gateCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	cps, err := s.controlPlanes(gateCtx)
	if err != nil {
		return err
	}

	if !slices.Contains(cps, node) {
		return fmt.Errorf("%s is not a control plane: etcd is recovered on a control plane", node)
	}

	ov := fetchEtcd(gateCtx, s.client, cps)

	if alive := liveMembers(ov); len(alive) > 0 {
		return fmt.Errorf("etcd still answers on %s: a recovery is only for a cluster that lost etcd (fix or remove members instead)", strings.Join(alive, ", "))
	}

	return nil
}

// liveMembers are the nodes whose etcd member answered its status.
func liveMembers(ov etcdOverview) []string {
	var out []string

	for _, st := range ov.Statuses {
		if st.Error == "" && st.MemberID != "" {
			out = append(out, st.Node)
		}
	}

	return out
}

// uploadSnapshot streams the file, decrypted on the fly, into the node's EtcdRecover.
func uploadSnapshot(
	ctx context.Context, s *session, req recoverRequest, decrypt func(io.Reader) (io.Reader, error),
	info snapshotInfo, report func(string, string, int64),
) error {
	f, err := os.Open(req.path)
	if err != nil {
		return fmt.Errorf("cannot open the snapshot: %w", err)
	}

	defer f.Close() //nolint:errcheck

	read := &uploadCounter{r: f}

	if info.Encrypted {
		report(recoverDecrypting, "opening the encrypted snapshot", 0)
	}

	plain, err := decrypt(read)
	if err != nil {
		return err
	}

	report(recoverUploading, "uploading the snapshot to "+req.node, read.n.Load())

	done := make(chan struct{})
	defer close(done)

	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()

		for {
			select {
			case <-done:
				return
			case <-tick.C:
				report(recoverUploading, "uploading the snapshot to "+req.node, read.n.Load())
			}
		}
	}()

	if _, err := s.client.EtcdRecover(client.WithNode(ctx, req.node), plain); err != nil {
		if ctx.Err() != nil {
			return errors.New("recovery stopped during the upload: nothing was bootstrapped")
		}

		return fmt.Errorf("uploading the snapshot failed, nothing was bootstrapped: %s", s.friendlyErr(req.node, err))
	}

	return nil
}

// waitRecovered waits until node's new etcd member answers.
func waitRecovered(ctx context.Context, s *session, node string, report func(string, string, int64), total int64) error {
	report(recoverWaiting, "waiting for etcd to start on "+node, total)

	waitCtx, cancel := context.WithTimeout(ctx, etcdRecoverWait)
	defer cancel()

	for {
		probeCtx, probeCancel := context.WithTimeout(waitCtx, callTimeout)
		ov := fetchEtcd(probeCtx, s.client, []string{node})

		probeCancel()

		if len(liveMembers(ov)) > 0 {
			report(recoverDone, "etcd runs again on "+node+" with one member: reset the other control planes so they join it", total)

			return nil
		}

		select {
		case <-waitCtx.Done():
			return errors.New("the bootstrap was accepted, but etcd did not answer within 5 minutes: check the etcd service logs on " + node)
		case <-time.After(etcdRecoverPoll):
		}
	}
}

// uploadCounter counts the bytes read through it, for the upload progress (read from the
// ticker goroutine, so atomic).
type uploadCounter struct {
	r io.Reader
	n atomic.Int64
}

func (c *uploadCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))

	return n, err
}
