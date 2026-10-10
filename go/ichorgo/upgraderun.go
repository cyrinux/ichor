package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

const (
	upgradeTimeout = 30 * time.Minute
	// upgradeRequestTimeout covers the request itself: Talos pulls and validates the
	// installer image before it answers.
	upgradeRequestTimeout = 10 * time.Minute
	upgradePollInterval   = 5 * time.Second
	// oldVersionGrace is how long the node may run its old version again after a reboot
	// before the upgrade is reported as failed (a staged upgrade reboots twice).
	oldVersionGrace = 5 * time.Minute
)

// Upgrade phases reported through UpgradeListener.OnProgress.
const (
	phaseRequested = "requested"
	phaseInstall   = "installing"
	phaseRebooting = "rebooting"
	phaseWaiting   = "waiting for node"
	phaseBooted    = "booted"
	phaseDone      = "done"
)

// UpgradeListener follows a Talos upgrade (implemented in Kotlin/Swift).
type UpgradeListener interface {
	// OnProgress gets {"phase","message","at"} (at: unix ms) on every phase change.
	OnProgress(json string)
	// OnDone is called exactly once: newVersion is the version the node booted ("" if
	// unknown), errMessage is empty on success.
	OnDone(newVersion string, errMessage string)
}

// UpgradeRun is a handle on a followed upgrade.
type UpgradeRun struct {
	cancel context.CancelFunc
}

// Cancel stops following the upgrade. It cannot cancel the upgrade itself once requested.
func (r *UpgradeRun) Cancel() { r.cancel() }

type upgradeProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

// errStoppedFollowing is reported when Cancel is used after the request was sent.
var errStoppedFollowing = errors.New("stopped following: the upgrade itself cannot be cancelled and continues on the node")

// StartUpgrade upgrades node to the installer image (see UpgradeImage), like
// `talosctl upgrade --image IMAGE` (os:admin), and follows it until the node runs the new
// version (30 min at most). It first re-runs UpgradePlan and refuses when there are
// blockers; force skips only the etcd checks (Talos's own --force semantics: it may lose
// etcd quorum), never the others. stage writes the upgrade to apply on the next boot, then
// reboots (Talos's --stage, for nodes with files in use). Talos cordons and drains the node,
// installs, then reboots it.
//
// It uses MachineService.Upgrade while the node serves it (planned removal: Talos 1.18) and
// falls back to the LifecycleService when the node answers Unimplemented: see requestUpgrade
// for what differs.
//
// The plan's acknowledgments (and UpgradeVersionCheck of the image's version) are refused
// unless acknowledged, whatever force. The run holds the cluster upgrade lock (see
// upgradelock.go) through the Kubernetes API (kubeServer: see KubePods); it goes on without
// it when that API cannot be used.
func StartUpgrade(configYAML, contextName, kubeServer, node, image string, stage, force, acknowledged bool, listener UpgradeListener) *UpgradeRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedUpgradeListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })

		opts := upgradeOptions{image: strings.TrimSpace(image), stage: stage, force: force, acknowledged: acknowledged}

		var version string

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "upgrade", Node: node, Params: fmt.Sprintf("image=%s stage=%t force=%t", opts.image, stage, force)}
		}, func() (err error) {
			version, err = runUpgrade(ctx, kubeTarget{configYAML, contextName, kubeServer}, node, opts, listener)

			return err
		})

		errMessage := errText(err)

		listener.OnDone(version, errMessage)
	}()

	return &UpgradeRun{cancel: cancel}
}

func emitProgress(l UpgradeListener, phase, message string) {
	emitJSON(upgradeProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, l.OnProgress)
}

// upgradeOptions are what StartUpgrade was asked.
type upgradeOptions struct {
	image                      string
	stage, force, acknowledged bool
}

func runUpgrade(ctx context.Context, kube kubeTarget, node string, o upgradeOptions, listener UpgradeListener) (version string, err error) {
	image := o.image

	repo, tag := splitImageRef(image)
	if repo == "" || tag == "" && !strings.Contains(image, "@") {
		return "", fmt.Errorf("invalid installer image %q", image)
	}

	s, release, err := acquireNode(kube.config, kube.context, node)
	if err != nil {
		return "", err
	}

	defer release()

	// The lock is not read here: taking it below is what keeps two runs apart.
	planCtx, planCancel := context.WithTimeout(ctx, planTimeout)
	plan := gatherPlan(planCtx, s, node, kubeTarget{})

	planCancel()

	if err := upgradeRefusal(plan, o.force); err != nil {
		return "", err
	}

	if err := acknowledgmentRefusal(plan, tag, o.acknowledged); err != nil {
		return "", err
	}

	if ctx.Err() != nil {
		return "", errors.New("cancelled before the upgrade was requested")
	}

	emit := func(phase, msg string) { emitProgress(listener, phase, msg) }

	lock, err := takeUpgradeLock(ctx, kube,
		upgradeLockRequest{holder: newLockHolder(), node: node, hostname: plan.Hostname, from: plan.CurrentVersion, to: tag},
		func(info *upgradeLockInfo) bool {
			return lockSettled(info, append([]planPeer{plan.target}, plan.peers...))
		},
		func(msg string) { emit(phaseRequested, msg) })
	if err != nil {
		return "", err
	}

	// A run the user stopped following is still upgrading: its lock expires on its own.
	defer func() {
		if !errors.Is(err, errStoppedFollowing) {
			lock.release()
		}
	}()

	emit(phaseRequested, "requesting the upgrade to "+image+"; the node pulls and checks the installer image first")

	// The request is not tied to Cancel: once sent, it completes.
	reqCtx, reqCancel := context.WithTimeout(context.Background(), upgradeRequestTimeout)
	defer reqCancel()

	// The node is about to change version: what the session cached about it is void.
	defer func() {
		s.versions.Delete(node)
		s.definitions.Delete(node)
	}()

	if err := requestUpgrade(client.WithNode(reqCtx, node), talosUpgrader{s.client}, image, o.stage, o.force, false, func() error { return upgradeRefusal(plan, false) }, emit); err != nil {
		if isUnavailableAPI(err) {
			return "", errors.New("upgrade: " + s.friendly(node, err))
		}

		return "", err
	}

	tracker := &upgradeTracker{
		oldVersion: plan.CurrentVersion,
		reinstall:  tag != "" && sameVersion(tag, plan.CurrentVersion),
		staged:     o.stage,
	}
	observe := func(ctx context.Context) upgradeObservation { return observeNode(ctx, s.client, node) }

	return followUpgrade(ctx, tracker, observe, emit, upgradePollInterval, time.Now)
}

// acknowledgmentRefusal refuses the plan's acknowledgments, and the risk of going to
// toVersion, unless the user acknowledged them.
func acknowledgmentRefusal(plan upgradePlan, toVersion string, acknowledged bool) error {
	if risk := versionRisk(plan.CurrentVersion, toVersion); risk != "" {
		return plan.checks().unconfirmed("upgrade", acknowledged, risk)
	}

	return plan.checks().unconfirmed("upgrade", acknowledged)
}

// upgradeRefusal returns the plan's blockers as an error, minus the etcd ones when forced.
func upgradeRefusal(plan upgradePlan, force bool) error {
	if force {
		return plan.checks().blocked("upgrade", plan.etcdBlockers)
	}

	return plan.checks().blocked("upgrade", nil)
}

// upgradeObservation is one poll of the node during the upgrade.
type upgradeObservation struct {
	reachable bool
	version   string
	stage     string
	ready     bool
}

func observeNode(ctx context.Context, c *client.Client, node string) upgradeObservation {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)

	resp, err := c.Version(nodeCtx)
	if err != nil {
		return upgradeObservation{}
	}

	o := upgradeObservation{reachable: true, version: first(resp.GetMessages()).GetVersion().GetTag()}

	if ms, err := safe.StateGetByID[*runtime.MachineStatus](nodeCtx, c.COSI, runtime.MachineStatusID); err == nil {
		o.stage, o.ready = ms.TypedSpec().Stage.String(), ms.TypedSpec().Status.Ready
	}

	return o
}

// upgradeTracker turns observations into phases.
type upgradeTracker struct {
	oldVersion string
	phase      string
	reinstall  bool      // the image carries the version the node already runs
	staged     bool      // staged upgrade: the node reboots twice
	sawDown    bool      // the node went down (rebooted) since the request
	inDown     bool      // the last observation was part of a reboot
	downs      int       // reboots seen since the request
	oldSince   time.Time // since when it runs the old version again after a reboot
}

// trackStep is what one observation changed.
type trackStep struct {
	phase, message string // phase is "" when unchanged
	done           bool
	newVersion     string
	err            error
}

func (t *upgradeTracker) step(o upgradeObservation, now time.Time) trackStep {
	var phase, msg string

	backOnOld := t.sawDown && o.reachable && sameVersion(o.version, t.oldVersion) &&
		o.stage == runtime.MachineStageRunning.String()
	if !backOnOld {
		t.oldSince = time.Time{}
	}

	rebooting := !o.reachable || o.stage == runtime.MachineStageRebooting.String() ||
		o.stage == runtime.MachineStageShuttingDown.String() || o.stage == runtime.MachineStageBooting.String()
	if rebooting && !t.inDown {
		t.downs++
	}

	t.inDown = rebooting

	switch {
	case !o.reachable:
		if t.sawDown {
			phase, msg = phaseWaiting, "waiting for the node to answer"
		} else {
			phase, msg = phaseRebooting, "the node is rebooting"
		}

		t.sawDown = true
	case o.stage == runtime.MachineStageRebooting.String() || o.stage == runtime.MachineStageShuttingDown.String():
		t.sawDown = true
		phase, msg = phaseRebooting, "the node is rebooting"
	case o.stage == runtime.MachineStageUpgrading.String() || o.stage == runtime.MachineStageInstalling.String():
		phase, msg = phaseInstall, "installing (still running "+o.version+")"
	case o.stage == runtime.MachineStageBooting.String():
		t.sawDown = true
		phase, msg = phaseWaiting, "the node is booting "+o.version
	case !sameVersion(o.version, t.oldVersion):
		if o.stage == runtime.MachineStageRunning.String() && o.ready {
			return t.change(trackStep{phase: phaseDone, message: "running " + o.version, done: true, newVersion: o.version})
		}

		phase, msg = phaseBooted, "booted "+o.version+", waiting for it to be ready"
	case backOnOld:
		if t.oldSince.IsZero() {
			t.oldSince = now
		}

		if t.reinstall {
			return t.change(t.reinstalled(o, now))
		}

		// Back on the old version after a reboot: a staged upgrade reboots once more, anything
		// else failed (Talos rolls back to the previous image when the new one does not boot).
		if now.Sub(t.oldSince) > oldVersionGrace {
			return trackStep{err: fmt.Errorf("the node rebooted but still runs %s: the upgrade failed or was rolled back (see its kernel log)", o.version)}
		}

		phase, msg = phaseWaiting, "the node is back on "+o.version+", waiting for the upgrade to apply"
	default:
		phase, msg = phaseInstall, "draining and installing"
	}

	return t.change(trackStep{phase: phase, message: msg})
}

// reinstalled handles a node that is back, after a reboot, on the version it was reinstalled
// with: the version cannot tell success, so being ready again does. A staged reinstall
// reboots twice: it is done after the second reboot, or once the node stayed up for
// oldVersionGrace when the two reboots were seen as one.
func (t *upgradeTracker) reinstalled(o upgradeObservation, now time.Time) trackStep {
	if !o.ready {
		return trackStep{phase: phaseBooted, message: "booted " + o.version + ", waiting for it to be ready"}
	}

	if t.staged && t.downs < 2 && now.Sub(t.oldSince) <= oldVersionGrace {
		return trackStep{phase: phaseWaiting, message: "the node is back on " + o.version + ", waiting for the staged reinstall to apply"}
	}

	return trackStep{phase: phaseDone, message: "running " + o.version + " (reinstalled)", done: true, newVersion: o.version}
}

// change reports the step only when its phase differs from the current one.
func (t *upgradeTracker) change(s trackStep) trackStep {
	if s.phase == t.phase && !s.done {
		s.phase = ""

		return s
	}

	t.phase = s.phase

	return s
}

func sameVersion(a, b string) bool {
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}

// followUpgrade polls the node until the tracker is done, fails, or ctx ends.
func followUpgrade(
	ctx context.Context, t *upgradeTracker, observe func(context.Context) upgradeObservation,
	emit func(phase, msg string), interval time.Duration, now func() time.Time,
) (string, error) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	booted := ""

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				if booted != "" {
					return booted, fmt.Errorf("the node booted %s but is not ready after %s", booted, upgradeTimeout)
				}

				return "", fmt.Errorf("the node did not come back with a new version within %s", upgradeTimeout)
			}

			return booted, errStoppedFollowing
		case <-timer.C:
		}

		o := observe(ctx)
		if ctx.Err() != nil {
			continue
		}

		s := t.step(o, now())
		if s.err != nil {
			return "", s.err
		}

		if s.phase != "" {
			emit(s.phase, s.message)
		}

		if t.phase == phaseBooted {
			booted = o.version
		}

		if s.done {
			return s.newVersion, nil
		}

		timer.Reset(interval)
	}
}
