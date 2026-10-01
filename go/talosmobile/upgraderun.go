package talosmobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
// It uses MachineService.Upgrade, which Talos 1.14 still serves but deprecates for its
// LifecycleService (removal planned in 1.18): the new API needs the client to drain the node
// through Kubernetes and has no server-side etcd checks.
func StartUpgrade(configYAML, contextName, node, image string, stage, force bool, listener UpgradeListener) *UpgradeRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedUpgradeListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)

	go func() {
		defer cancel()

		version, err := runUpgrade(ctx, configYAML, contextName, node, strings.TrimSpace(image), stage, force, listener)

		errMessage := ""
		if err != nil {
			errMessage = err.Error()
		}

		listener.OnDone(version, errMessage)
	}()

	return &UpgradeRun{cancel: cancel}
}

func emitProgress(l UpgradeListener, phase, message string) {
	b, err := json.Marshal(upgradeProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()})
	if err == nil {
		l.OnProgress(string(b))
	}
}

func runUpgrade(
	ctx context.Context, configYAML, contextName, node, image string, stage, force bool, listener UpgradeListener,
) (string, error) {
	if repo, tag := splitImageRef(image); repo == "" || tag == "" && !strings.Contains(image, "@") {
		return "", fmt.Errorf("invalid installer image %q", image)
	}

	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		return "", err
	}

	defer release()

	if err := validatePowerTarget(s.context, node); err != nil {
		return "", err
	}

	planCtx, planCancel := context.WithTimeout(ctx, planTimeout)
	plan := gatherPlan(planCtx, s, node)

	planCancel()

	if err := upgradeRefusal(plan, force); err != nil {
		return "", err
	}

	if ctx.Err() != nil {
		return "", errors.New("cancelled before the upgrade was requested")
	}

	emitProgress(listener, phaseRequested, "requesting the upgrade to "+image+"; the node pulls and checks the installer image first")

	// The request is not tied to Cancel: once sent, it completes.
	reqCtx, reqCancel := context.WithTimeout(context.Background(), upgradeRequestTimeout)
	defer reqCancel()

	if err := requestUpgrade(withNode(reqCtx, node), s.client, image, stage, force); err != nil {
		return "", err
	}

	emitProgress(listener, phaseInstall, "upgrade accepted: draining and installing")

	_, tag := splitImageRef(image)
	tracker := &upgradeTracker{
		oldVersion: plan.CurrentVersion,
		reinstall:  tag != "" && sameVersion(tag, plan.CurrentVersion),
		staged:     stage,
	}
	observe := func(ctx context.Context) upgradeObservation { return observeNode(ctx, s.client, node) }

	return followUpgrade(ctx, tracker, observe, func(phase, msg string) { emitProgress(listener, phase, msg) },
		upgradePollInterval, time.Now)
}

// upgradeRefusal returns the plan's blockers as an error, minus the etcd ones when forced.
func upgradeRefusal(plan upgradePlan, force bool) error {
	var blockers []string

	for _, b := range plan.Blockers {
		if force && slices.Contains(plan.etcdBlockers, b) {
			continue
		}

		blockers = append(blockers, b)
	}

	if len(blockers) == 0 {
		return nil
	}

	return errors.New("upgrade refused: " + strings.Join(blockers, "; "))
}

func requestUpgrade(ctx context.Context, c *client.Client, image string, stage, force bool) error {
	//nolint:staticcheck // see StartUpgrade: the LifecycleService path lacks drain and etcd checks
	_, err := c.UpgradeWithOptions(ctx,
		client.WithUpgradeImage(image),
		client.WithUpgradeRebootMode(machineapi.UpgradeRequest_DEFAULT),
		client.WithUpgradeStage(stage),
		client.WithUpgradeForce(force),
	)
	if err == nil {
		return nil
	}

	if status.Code(err) == codes.Unimplemented {
		return errors.New("this node does not offer the legacy upgrade API: upgrade it with talosctl")
	}

	return errors.New("upgrade request failed: " + friendlyError(err))
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

	nodeCtx := withNode(ctx, node)

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

type maskedUpgradeListener struct{ UpgradeListener }

func (l maskedUpgradeListener) OnProgress(json string) {
	l.UpgradeListener.OnProgress(privacy.mask(json))
}

func (l maskedUpgradeListener) OnDone(newVersion string, errMessage string) {
	l.UpgradeListener.OnDone(newVersion, privacy.maskPlain(errMessage))
}
