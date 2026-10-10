package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// The NOSPACE fix: snapshot → defragment every member (followers first, the leader last) →
// disarm the alarm → read etcd again, as one followed run.

// etcdFixTimeout bounds the whole run: a snapshot and one defragmentation per member.
const etcdFixTimeout = snapshotTimeout + 15*time.Minute

// Phases of the NOSPACE fix, in order.
const (
	fixSnapshot = "snapshot"
	fixDefrag   = "defrag"
	fixDisarm   = "disarm"
	fixRecheck  = "recheck"
	fixSteps    = 4
)

// Member states during the defragmentation.
const (
	fixPending = "pending"
	fixRunning = "running"
	fixDone    = "done"
	fixFailed  = "failed"
)

// EtcdFixListener follows the NOSPACE fix (implemented in Kotlin/Swift).
type EtcdFixListener interface {
	// OnProgress gets {"phase","message","at","step","steps","members":[{"node","hostname",
	// "state","reclaimedBytes"}]} on every change; step is 1-based.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty on success.
	OnDone(errMessage string)
}

// EtcdFixRun is a handle on a running NOSPACE fix.
type EtcdFixRun struct {
	cancel context.CancelFunc
}

// Cancel stops the fix before its next step (a defragmentation under way finishes).
func (r *EtcdFixRun) Cancel() { r.cancel() }

type etcdFixProgress struct {
	Phase   string          `json:"phase"`
	Message string          `json:"message"`
	At      int64           `json:"at"`
	Step    int             `json:"step"`
	Steps   int             `json:"steps"`
	Members []etcdFixMember `json:"members"`
}

type etcdFixMember struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	State    string `json:"state"`
	// ReclaimedBytes is what the member's database shrank by, known once etcd is read again.
	ReclaimedBytes int64 `json:"reclaimedBytes"`

	dbSize int64
}

// StartEtcdNospaceFix clears etcd's NOSPACE alarm the way the etcd docs say (os:admin):
// a snapshot of the database through snapshotNode into destPath (age-encrypted for
// recipients or passphrase, as StartEtcdSnapshotEncrypted; in clear when both are empty;
// skipped when destPath is empty), a defragmentation of every member one at a time,
// followers first and the leader last, the alarm disarmed, then etcd read again. It
// refuses to start without an active NOSPACE alarm and stops at the first failure.
func StartEtcdNospaceFix(configYAML, contextName, snapshotNode, destPath, recipients, passphrase string, listener EtcdFixListener) *EtcdFixRun {
	contextName, snapshotNode = unmaskTarget(configYAML, contextName, snapshotNode)

	listener = maskedEtcdFixListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), etcdFixTimeout)

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		snapshot := "no"
		if destPath != "" {
			snapshot = "yes"
		}

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "etcd-nospace-fix", Node: snapshotNode, Params: "snapshot=" + snapshot}
		}, func() error {
			return talosEtcdFix(ctx, configYAML, contextName, snapshotNode, destPath, recipients, passphrase, listener)
		})

		listener.OnDone(errText(err))
	}()

	return &EtcdFixRun{cancel: cancel}
}

// etcdFixer is what the fix needs from the cluster; tests replace it.
type etcdFixer interface {
	overview(ctx context.Context) (etcdOverview, error)
	snapshot(ctx context.Context, node string, progress func(int64)) error
	defrag(ctx context.Context, node string) error
	disarm(ctx context.Context, node string) error
}

func talosEtcdFix(ctx context.Context, configYAML, contextName, snapshotNode, destPath, recipients, passphrase string, listener EtcdFixListener) error {
	var wrap snapshotWrap

	if destPath != "" && (strings.TrimSpace(recipients) != "" || passphrase != "") {
		w, err := snapshotEncryptor(recipients, passphrase)
		if err != nil {
			return err
		}

		wrap = w
	}

	s, release, err := acquireSession(configYAML, contextName)
	if err != nil {
		return err
	}

	defer release()

	if snapshotNode != "" {
		if err := validatePowerTarget(s.context, snapshotNode); err != nil {
			return err
		}
	}

	fixer := talosFixer{s: s, config: configYAML, context: contextName, destPath: destPath, wrap: wrap}

	return runEtcdFix(ctx, fixer, snapshotNode, destPath != "", func(p etcdFixProgress) { emitJSON(p, listener.OnProgress) })
}

// runEtcdFix runs the steps through f; takeSnapshot false skips the snapshot (said so).
func runEtcdFix(ctx context.Context, f etcdFixer, snapshotNode string, takeSnapshot bool, emit func(etcdFixProgress)) error {
	before, err := f.overview(ctx)
	if err != nil {
		return err
	}

	if err := nospaceRefusal(before); err != nil {
		return err
	}

	members := defragMembers(before)
	if len(members) == 0 {
		return errors.New("no etcd member answered: nothing to defragment")
	}

	report := func(phase string, step int, msg string) {
		emit(etcdFixProgress{Phase: phase, Message: msg, At: time.Now().UnixMilli(), Step: step, Steps: fixSteps, Members: slices.Clone(members)})
	}

	stopped := func(step string) error {
		if ctx.Err() != nil {
			return errors.New("stopped before the " + step + " step")
		}

		return nil
	}

	if err := fixTakeSnapshot(ctx, f, snapshotNode, members, takeSnapshot, report); err != nil {
		return err
	}

	if err := stopped(fixDefrag); err != nil {
		return err
	}

	if err := fixDefragAll(ctx, f, members, report, stopped); err != nil {
		return err
	}

	if err := stopped(fixDisarm); err != nil {
		return err
	}

	// Alarms are cluster-wide: the first member defragmented answers for them.
	report(fixDisarm, 3, "disarming the NOSPACE alarm")

	if err := f.disarm(ctx, members[0].Node); err != nil {
		return fmt.Errorf("disarming the alarm failed: %w", err)
	}

	return fixReadAgain(ctx, f, members, report)
}

func fixTakeSnapshot(ctx context.Context, f etcdFixer, node string, members []etcdFixMember, take bool, report func(string, int, string)) error {
	if !take {
		report(fixSnapshot, 1, "skipped: no snapshot is taken before the defragmentation")

		return nil
	}

	if node == "" {
		node = members[len(members)-1].Node // the leader: it has every write
	}

	report(fixSnapshot, 1, "taking a snapshot through "+node)

	if err := f.snapshot(ctx, node, func(bytes int64) {
		report(fixSnapshot, 1, fmt.Sprintf("snapshot: %s written", formatSize(uint64(bytes))))
	}); err != nil {
		return fmt.Errorf("the snapshot failed, nothing was changed: %w", err)
	}

	report(fixSnapshot, 1, "snapshot saved")

	return nil
}

func fixDefragAll(ctx context.Context, f etcdFixer, members []etcdFixMember, report func(string, int, string), stopped func(string) error) error {
	for i := range members {
		if i > 0 {
			if err := stopped(fixDefrag); err != nil {
				return err
			}
		}

		m := &members[i]
		m.State = fixRunning
		report(fixDefrag, 2, fmt.Sprintf("defragmenting %s (%d of %d)", m.Hostname, i+1, len(members)))

		if err := f.defrag(ctx, m.Node); err != nil {
			m.State = fixFailed
			report(fixDefrag, 2, "defragmenting "+m.Hostname+" failed")

			return fmt.Errorf("defragmenting %s failed, the alarm is still active: %w", m.Hostname, err)
		}

		m.State = fixDone
	}

	report(fixDefrag, 2, fmt.Sprintf("%d members defragmented", len(members)))

	return nil
}

func fixReadAgain(ctx context.Context, f etcdFixer, members []etcdFixMember, report func(string, int, string)) error {
	report(fixRecheck, 4, "reading etcd again")

	after, err := f.overview(ctx)
	if err != nil {
		return fmt.Errorf("the alarm was disarmed, but etcd could not be read again: %w", err)
	}

	sizes := map[string]int64{}
	for _, st := range after.Statuses {
		if st.Error == "" {
			sizes[st.Node] = st.DbSize
		}
	}

	var reclaimed int64

	for i := range members {
		if size, ok := sizes[members[i].Node]; ok && size < members[i].dbSize {
			members[i].ReclaimedBytes = members[i].dbSize - size
			reclaimed += members[i].ReclaimedBytes
		}
	}

	if after.AlarmsError != "" {
		return errors.New("the alarm was disarmed, but the alarms could not be read again: " + after.AlarmsError)
	}

	if hasNospace(after) {
		return errors.New("the NOSPACE alarm is still active: the database is still over its quota (raise quota-backend-bytes in the machine config, or delete data)")
	}

	msg := fmt.Sprintf("the alarm is cleared; the members gave back %s", formatSize(uint64(reclaimed)))
	if reclaimed == 0 {
		msg = "the alarm is cleared, but the database did not shrink: it may reach its quota again soon"
	}

	report(fixRecheck, 4, msg)

	return nil
}

// nospaceRefusal refuses a fix without an active NOSPACE alarm: the button is for it.
func nospaceRefusal(ov etcdOverview) error {
	switch {
	case ov.AlarmsError != "":
		return errors.New("cannot read the etcd alarms: " + ov.AlarmsError)
	case !hasNospace(ov):
		return errors.New("no NOSPACE alarm is active: there is nothing to fix")
	}

	return nil
}

func hasNospace(ov etcdOverview) bool {
	return slices.ContainsFunc(ov.Alarms, func(a etcdAlarm) bool { return a.Alarm == "NOSPACE" })
}

// defragMembers are the members to defragment, one at a time as Talos advises: followers
// first (most to reclaim first), the leader last (it stays available longest); members that
// could not be read are skipped. The apps used the same order (defragOrder).
func defragMembers(ov etcdOverview) []etcdFixMember {
	hostnames := map[string]string{}
	for _, m := range ov.Members {
		hostnames[m.ID] = m.Hostname
	}

	statuses := slices.DeleteFunc(slices.Clone(ov.Statuses), func(s etcdNodeStatus) bool { return s.Error != "" || s.MemberID == "" })

	slices.SortStableFunc(statuses, func(a, b etcdNodeStatus) int {
		if a.IsLeader != b.IsLeader {
			if a.IsLeader {
				return 1
			}

			return -1
		}

		return int((b.DbSize - b.DbSizeInUse) - (a.DbSize - a.DbSizeInUse))
	})

	out := make([]etcdFixMember, len(statuses))
	for i, s := range statuses {
		host := hostnames[s.MemberID]
		if host == "" {
			host = s.Node
		}

		out[i] = etcdFixMember{Node: s.Node, Hostname: host, State: fixPending, dbSize: s.DbSize}
	}

	return out
}

// talosFixer is etcdFixer on a Talos session.
type talosFixer struct {
	s               *session
	config, context string
	destPath        string
	wrap            snapshotWrap
}

func (t talosFixer) overview(ctx context.Context) (etcdOverview, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	return gatherEtcdOverview(ctx, t.s)
}

func (t talosFixer) snapshot(ctx context.Context, node string, progress func(int64)) error {
	_, _, err := runSnapshot(ctx, t.config, t.context, node, t.destPath, t.wrap, snapshotProgress(progress))

	return err
}

func (t talosFixer) defrag(ctx context.Context, node string) error {
	ctx, cancel := context.WithTimeout(ctx, defragTimeout)
	defer cancel()

	if _, err := t.s.client.EtcdDefragment(client.WithNode(ctx, node)); err != nil {
		return t.s.friendlyErr(node, err)
	}

	return nil
}

func (t talosFixer) disarm(ctx context.Context, node string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := t.s.client.EtcdAlarmDisarm(client.WithNode(ctx, node)); err != nil {
		return t.s.friendlyErr(node, err)
	}

	return nil
}

// snapshotProgress is a SnapshotListener that only forwards the progress.
type snapshotProgress func(int64)

func (p snapshotProgress) OnProgress(bytes int64)               { p(bytes) }
func (p snapshotProgress) OnDone(string, int64, string, string) {}
