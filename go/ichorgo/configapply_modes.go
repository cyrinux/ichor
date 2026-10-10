package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// Applying a machine config change for good, without a try: now (when it needs no reboot),
// at the next reboot (staged), or now with a reboot. See StartConfigApply.

const (
	applyModeAuto   = "auto"
	applyModeStaged = "staged"
	applyModeReboot = "reboot"

	applyPhaseApplying  = "applying"
	applyPhaseRebooting = "rebooting"
	applyPhaseWaiting   = "waiting"
	applyPhaseDone      = "done"

	// configApplyTimeout bounds a run: the apply, then for a reboot the wait for the node.
	configApplyTimeout = backTimeout + configTryMargin
)

var errConfigApplyNeedsReboot = errors.New("this change needs a reboot: apply it at the next reboot, or now with a reboot")

// ConfigApplyListener follows a config applied with StartConfigApply (implemented in Kotlin/Swift).
type ConfigApplyListener interface {
	// OnProgress gets {"phase","message","at"} on every phase change: applying, rebooting
	// and waiting (reboot mode), then done.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty on success.
	OnDone(errMessage string)
}

// ConfigApplyRun is a handle on a config being applied.
type ConfigApplyRun struct {
	cancel context.CancelFunc
}

// Cancel stops following the run. A config already sent stays applied (or staged), and a
// reboot already asked for goes on.
func (r *ConfigApplyRun) Cancel() { r.cancel() }

type configApplyProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

// StartConfigApply applies draftYAML to node for good, like `talosctl apply-config --mode
// MODE` (os:admin), and follows it. baseYAML and draftYAML are as in MachineConfigPreview,
// whose checks are made again. mode is:
//   - "auto": applied now; refused when the change needs a reboot (Talos would reboot);
//   - "staged": written to apply at the next reboot, which is not asked for;
//   - "reboot": applied, then the node reboots; the run waits until it runs again.
//
// Unlike StartConfigTry nothing is reverted by itself: a reboot is only asked for in mode
// "reboot", which the apps confirm with the typed hostname.
func StartConfigApply(configYAML, contextName, node, baseYAML, draftYAML, mode string, listener ConfigApplyListener) *ConfigApplyRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedConfigApplyListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), configApplyTimeout)

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "config-apply", Node: node, Params: "mode=" + mode}
		}, func() error {
			return startConfigApply(ctx, configYAML, contextName, node, configApply{
				base: baseYAML, draft: draftYAML, mode: strings.ToLower(strings.TrimSpace(mode)),
				emit: func(phase, message string) {
					emitJSON(configApplyProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, listener.OnProgress)
				},
			})
		})

		listener.OnDone(errText(err))
	}()

	return &ConfigApplyRun{cancel: cancel}
}

// configApply is what an apply run needs besides the node.
type configApply struct {
	base, draft, mode string
	emit              func(phase, message string)
	// waitBack waits for the node to reboot and run again (reboot mode); the run sets it.
	waitBack func(ctx context.Context) error
}

func startConfigApply(ctx context.Context, configYAML, contextName, node string, c configApply) error {
	if privacy.isEnabled() {
		return errConfigPrivacy
	}

	if isDemoContext(configYAML, contextName) {
		return errDemoUnavailable
	}

	s, release, err := acquireNode(configYAML, contextName, node)
	if err != nil {
		return err
	}

	defer release()

	c.waitBack = func(ctx context.Context) error { return waitNodeRebooted(ctx, s.client, node, c.emit) }

	return runConfigApply(ctx, nodeConfigApplier{s, node}, c)
}

func parseApplyMode(mode string) (machineapi.ApplyConfigurationRequest_Mode, error) {
	switch mode {
	case applyModeAuto:
		return machineapi.ApplyConfigurationRequest_AUTO, nil
	case applyModeStaged:
		return machineapi.ApplyConfigurationRequest_STAGED, nil
	case applyModeReboot:
		//lint:ignore SA1019 still the mode that applies and reboots, as `talosctl apply-config --mode reboot`
		return machineapi.ApplyConfigurationRequest_REBOOT, nil
	default:
		return 0, fmt.Errorf("unknown apply mode %q (auto, staged, reboot)", mode)
	}
}

func runConfigApply(ctx context.Context, a configApplier, c configApply) error {
	mode, err := parseApplyMode(c.mode)
	if err != nil {
		return err
	}

	snap, err := a.current(ctx)
	if err != nil {
		return err
	}

	plan, err := planConfigChange(snap, c.base, c.draft)
	if err != nil {
		return err
	}

	if !plan.changed() {
		return errConfigUnchanged
	}

	// The node validates the change first; AUTO is refused here, not by a surprise reboot.
	dry, err := a.apply(ctx, plan.data, machineapi.ApplyConfigurationRequest_AUTO, true, 0)
	if err != nil {
		return err
	}

	if c.mode == applyModeAuto && needsReboot(dry) {
		return errConfigApplyNeedsReboot
	}

	c.emit(applyPhaseApplying, "")

	if _, err := a.apply(ctx, plan.data, mode, false, 0); err != nil {
		// A rebooting node may drop the answer: only a node still showing its previous
		// config has refused.
		if c.mode != applyModeReboot {
			return err
		}

		if now, readErr := a.current(ctx); readErr == nil && now.redacted == plan.before {
			return err
		}
	}

	switch c.mode {
	case applyModeStaged:
		c.emit(applyPhaseDone, "the change applies at the next reboot")
	case applyModeReboot:
		c.emit(applyPhaseRebooting, "the node reboots into the new config")

		if err := c.waitBack(ctx); err != nil {
			return fmt.Errorf("the config was applied, but %w", err)
		}

		c.emit(applyPhaseDone, "the node is back with the new config")
	default:
		c.emit(applyPhaseDone, "")
	}

	return nil
}

// waitNodeRebooted waits until node went down and runs again, ready (Talos only: the
// Kubernetes API may not be reachable from the phone).
func waitNodeRebooted(ctx context.Context, c *client.Client, node string, emit func(phase, message string)) error {
	backCtx, cancel := context.WithTimeout(ctx, backTimeout)
	defer cancel()

	err := waitBack(backCtx, func(ctx context.Context) backObservation {
		o := observeNode(ctx, c, node)
		running := o.stage == runtime.MachineStageRunning.String() && o.ready

		return backObservation{reachable: o.reachable, running: running, kubeReady: running}
	}, func(msg string) { emit(applyPhaseWaiting, msg) }, upgradePollInterval)

	if err != nil && ctx.Err() == nil && errors.Is(backCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the node is not back within %s", backTimeout)
	}

	return err
}
