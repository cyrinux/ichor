package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Changing a node's machine config from the app: the edited draft is compared with the
// node's config (MachineConfigPreview), then applied in Talos's "try" mode (StartConfigTry),
// like `talosctl apply-config --mode try --timeout 5m`: no reboot, and the node goes back to
// its previous config by itself unless the change is kept before the timeout. A phone that
// loses the node after a bad change therefore heals it by doing nothing.

const (
	configPreviewTimeout = 40 * time.Second
	// configTryMargin is how long a run outlives the try: the revert check and its calls.
	configTryMargin = 2 * time.Minute
	// configRevertChecks times configRevertWait is how long the node is given, after the
	// timeout, to show its previous config again.
	configRevertChecks = 5
	configRevertWait   = 3 * time.Second

	diffLineHunk    = "hunk"
	diffLineContext = "context"
	diffLineAdded   = "added"
	diffLineRemoved = "removed"

	tryPhaseApplying  = "applying"
	tryPhaseTrying    = "trying"
	tryPhaseKeeping   = "keeping"
	tryPhaseReverting = "reverting"

	tryOutcomeKept     = "kept"
	tryOutcomeReverted = "reverted"

	tryCommandKeep   = "keep"
	tryCommandRevert = "revert"
)

var (
	errConfigChanged     = errors.New("the node's config changed since it was loaded: reload it and edit again")
	errConfigPrivacy     = errors.New("turn privacy mode off to edit the machine config: masked names and addresses must not be sent to a node")
	errConfigUnchanged   = errors.New("the edited config is the same as the node's")
	errConfigNeedsReboot = errors.New("this change needs a reboot, so it cannot be tried with an automatic revert")
	errStoppedTry        = errors.New("stopped following: unless it was kept, the node reverts the change by itself at the end of the timeout")
)

// configTryTimeouts are the try durations offered, in seconds.
var configTryTimeouts = []int{60, 300, 600}

// configSnapshot is a node's config as read at one moment.
type configSnapshot struct {
	raw      []byte // as the node holds it: what a revert sends back
	plain    string // with its secrets
	redacted string // what NodeMachineConfig shows
}

type configApplyResult struct {
	mode machineapi.ApplyConfigurationRequest_Mode
}

// configApplier reads and writes one node's config (a fake in the tests).
type configApplier interface {
	current(ctx context.Context) (configSnapshot, error)
	apply(ctx context.Context, data []byte, mode machineapi.ApplyConfigurationRequest_Mode, dryRun bool, tryFor time.Duration) (configApplyResult, error)
}

func snapshotOf(provider talosconfig.Provider) (configSnapshot, error) {
	raw, err := provider.Bytes()
	if err != nil {
		return configSnapshot{}, fmt.Errorf("read machine config: %w", err)
	}

	plain, err := provider.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return configSnapshot{}, fmt.Errorf("encode machine config: %w", err)
	}

	hidden, err := provider.RedactSecrets(redacted).EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return configSnapshot{}, fmt.Errorf("encode machine config: %w", err)
	}

	return configSnapshot{raw: raw, plain: plain, redacted: hidden}, nil
}

type nodeConfigApplier struct {
	s    *session
	node string
}

func (a nodeConfigApplier) current(ctx context.Context) (configSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	mc, err := safe.StateGetByID[*config.MachineConfig](client.WithNode(ctx, a.node), a.s.client.COSI, config.ActiveID)
	if err != nil {
		return configSnapshot{}, a.s.friendlyErr(a.node, err)
	}

	return snapshotOf(mc.Provider())
}

func (a nodeConfigApplier) apply(ctx context.Context, data []byte, mode machineapi.ApplyConfigurationRequest_Mode, dryRun bool, tryFor time.Duration) (configApplyResult, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	req := &machineapi.ApplyConfigurationRequest{Data: data, Mode: mode, DryRun: dryRun}
	if tryFor > 0 {
		req.TryModeTimeout = durationpb.New(tryFor)
	}

	resp, err := a.s.client.ApplyConfiguration(client.WithNode(ctx, a.node), req)
	if err != nil {
		return configApplyResult{}, a.s.friendlyErr(a.node, err)
	}

	// The answer's details are not kept: a dry run puts the config diff, secrets included,
	// in them.
	return configApplyResult{mode: first(resp.GetMessages()).GetMode()}, nil
}

// demoConfigApplier lets the demo cluster preview a change; nothing can be applied to it.
type demoConfigApplier struct{ text string }

func (a demoConfigApplier) current(context.Context) (configSnapshot, error) {
	return configSnapshot{raw: []byte(a.text), plain: a.text, redacted: a.text}, nil
}

func (a demoConfigApplier) apply(_ context.Context, _ []byte, _ machineapi.ApplyConfigurationRequest_Mode, dryRun bool, _ time.Duration) (configApplyResult, error) {
	if !dryRun {
		return configApplyResult{}, errDemoUnavailable
	}

	return configApplyResult{mode: machineapi.ApplyConfigurationRequest_NO_REBOOT}, nil
}

// canonicalConfig is text as Talos writes a config (what a node gives back), so a draft made
// from it compares line by line with the result of an edit.
func canonicalConfig(text string) (string, error) {
	provider, err := configloader.NewFromBytes([]byte(text))
	if err != nil {
		return "", err
	}

	return provider.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
}

// configPlan is a draft made ready for a node.
type configPlan struct {
	data          []byte // the draft with its secrets back: what is sent
	before, after string // redacted, for the diff
}

func (p configPlan) changed() bool { return p.before != p.after }

// planConfigChange checks draftYAML against the node's config snap and prepares what to
// send. baseYAML is the redacted config the draft was made from: when the node no longer
// holds it, somebody else changed the config and applying the draft would undo their change.
func planConfigChange(snap configSnapshot, baseYAML, draftYAML string) (configPlan, error) {
	if strings.TrimSpace(baseYAML) != strings.TrimSpace(snap.redacted) {
		return configPlan{}, errConfigChanged
	}

	data, err := restoreSecrets(snap.plain, snap.redacted, draftYAML)
	if err != nil {
		return configPlan{}, err
	}

	provider, err := configloader.NewFromBytes([]byte(data))
	if err != nil {
		return configPlan{}, fmt.Errorf("the edited config is not a valid machine config: %w", err)
	}

	after, err := provider.RedactSecrets(redacted).EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return configPlan{}, fmt.Errorf("encode machine config: %w", err)
	}

	return configPlan{data: []byte(data), before: snap.redacted, after: after}, nil
}

type configDiffLine struct {
	Kind string `json:"kind"` // hunk, context, added, removed
	Text string `json:"text"`
}

type configPreview struct {
	Changed bool             `json:"changed"`
	Lines   []configDiffLine `json:"lines"`
	// NeedsReboot: Talos would reboot the node to apply this, so it cannot be tried.
	NeedsReboot bool `json:"needsReboot"`
}

// MachineConfigPreview tells what applying draftYAML to node would change, without changing
// anything: {"changed","lines":[{"kind","text"}],"needsReboot"}. baseYAML is the config the
// draft was made from, as NodeMachineConfig returned it without revealSecrets; the call is
// refused when the node's config is no longer that one. The hidden secrets of the draft are
// put back from the node's config (changing or removing a secret is refused), the result is
// validated by the node itself with an ApplyConfiguration dry run (os:admin), and the diff
// (lines of kind hunk, context, added, removed) is made of the redacted configs. Refused in
// privacy mode, whose masked values must not reach a node.
func MachineConfigPreview(configYAML, contextName, node, baseYAML, draftYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if privacy.isEnabled() {
		return "", errConfigPrivacy
	}

	if isDemoContext(configYAML, contextName) {
		preview, err := previewConfig(context.Background(), demoConfigApplier{baseYAML}, baseYAML, draftYAML)
		if err != nil {
			return "", err
		}

		return toJSON(preview)
	}

	return withNodeSession(configYAML, contextName, node, configPreviewTimeout, func(ctx context.Context, s *session) (string, error) {
		preview, err := previewConfig(ctx, nodeConfigApplier{s, node}, baseYAML, draftYAML)
		if err != nil {
			return "", err
		}

		return toJSON(preview)
	})
}

func previewConfig(ctx context.Context, a configApplier, baseYAML, draftYAML string) (configPreview, error) {
	snap, err := a.current(ctx)
	if err != nil {
		return configPreview{}, err
	}

	plan, err := planConfigChange(snap, baseYAML, draftYAML)
	if err != nil {
		return configPreview{}, err
	}

	preview := configPreview{Changed: plan.changed(), Lines: configDiffLines(plan.before, plan.after)}
	if !preview.Changed {
		return preview, nil
	}

	result, err := a.apply(ctx, plan.data, machineapi.ApplyConfigurationRequest_AUTO, true, 0)
	if err != nil {
		return configPreview{}, err
	}

	preview.NeedsReboot = needsReboot(result)

	return preview, nil
}

// needsReboot reads the mode Talos chose for a config applied in AUTO mode.
func needsReboot(r configApplyResult) bool {
	return r.mode == machineapi.ApplyConfigurationRequest_REBOOT
}

// configDiffLines is the unified diff of two configs, line by line.
func configDiffLines(before, after string) []configDiffLine {
	lines := []configDiffLine{}

	for i, l := range diffSplitLines(unifiedDiff(before, after, "node", "edited")) {
		switch {
		case i < 2: // the "--- node" and "+++ edited" header
		case strings.HasPrefix(l, "@@"):
			lines = append(lines, configDiffLine{diffLineHunk, l})
		case strings.HasPrefix(l, "+"):
			lines = append(lines, configDiffLine{diffLineAdded, l[1:]})
		case strings.HasPrefix(l, "-"):
			lines = append(lines, configDiffLine{diffLineRemoved, l[1:]})
		default:
			lines = append(lines, configDiffLine{diffLineContext, strings.TrimPrefix(l, " ")})
		}
	}

	return lines
}

// ConfigTryListener follows a config applied in try mode (implemented in Kotlin/Swift).
type ConfigTryListener interface {
	// OnProgress gets {"phase","message","deadline","at"} on every phase change: phase is
	// applying, trying (deadline: when the node reverts, unix ms), keeping or reverting;
	// message is set when keeping or reverting failed and the try goes on.
	OnProgress(json string)
	// OnDone is called exactly once: outcome is "kept" or "reverted", or empty with
	// errMessage set.
	OnDone(outcome string, errMessage string)
}

// ConfigTryRun is a handle on a config being tried.
type ConfigTryRun struct {
	cancel   context.CancelFunc
	commands chan string
}

// Keep makes the tried config permanent (applies it again in AUTO mode).
func (r *ConfigTryRun) Keep() { r.send(tryCommandKeep) }

// Revert puts the previous config back now instead of waiting for the timeout.
func (r *ConfigTryRun) Revert() { r.send(tryCommandRevert) }

// Cancel stops following the try. It does not keep the change: the node reverts it by
// itself at the end of the timeout.
func (r *ConfigTryRun) Cancel() { r.cancel() }

func (r *ConfigTryRun) send(command string) {
	select {
	case r.commands <- command:
	default: // one is already waiting
	}
}

type configTryProgress struct {
	Phase    string `json:"phase"`
	Message  string `json:"message"`
	Deadline int64  `json:"deadline"`
	At       int64  `json:"at"`
}

// StartConfigTry applies draftYAML to node in try mode, like `talosctl apply-config --mode
// try --timeout`, and follows it (os:admin). baseYAML and draftYAML are as in
// MachineConfigPreview, whose checks are made again; timeoutSec is 60, 300 or 600. The node
// applies the config without a reboot and reverts to the previous one after the timeout,
// unless Keep is called first; Revert puts the previous config back at once. A change that
// needs a reboot is refused: it could not be reverted. If the app stops following (Cancel,
// app killed, network lost) the node still reverts by itself.
func StartConfigTry(configYAML, contextName, node, baseYAML, draftYAML string, timeoutSec int, listener ConfigTryListener) *ConfigTryRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedConfigTryListener{listener}

	timeout := time.Duration(timeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout+configTryMargin)
	run := &ConfigTryRun{cancel: cancel, commands: make(chan string, 1)}

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })

		outcome, err := startConfigTry(ctx, configYAML, contextName, node, configTry{base: baseYAML, draft: draftYAML, timeoutSec: timeoutSec, commands: run.commands, after: time.After, emit: func(phase, message string, deadline time.Time) {
			p := configTryProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}
			if !deadline.IsZero() {
				p.Deadline = deadline.UnixMilli()
			}

			emitJSON(p, listener.OnProgress)
		}})

		recordOutcome(configYAML, contextName, auditAction{Action: "config-try", Node: node, Params: configTryAuditParams(timeoutSec, outcome)}, err)

		listener.OnDone(outcome, errText(err))
	}()

	return run
}

// configTry is what a try run needs besides the node.
type configTry struct {
	base, draft string
	timeoutSec  int
	commands    <-chan string
	after       func(time.Duration) <-chan time.Time
	emit        func(phase, message string, deadline time.Time)
}

// configTryAuditParams sums a try up for the audit log: never the config, it holds secrets.
func configTryAuditParams(timeoutSec int, outcome string) string {
	params := fmt.Sprintf("timeout=%ds", timeoutSec)
	if outcome != "" {
		params += " outcome=" + outcome
	}

	return params
}

func startConfigTry(ctx context.Context, configYAML, contextName, node string, t configTry) (string, error) {
	if privacy.isEnabled() {
		return "", errConfigPrivacy
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	s, release, err := acquireNode(configYAML, contextName, node)
	if err != nil {
		return "", err
	}

	defer release()

	return runConfigTry(ctx, nodeConfigApplier{s, node}, t)
}

func runConfigTry(ctx context.Context, a configApplier, t configTry) (string, error) {
	valid := false
	for _, s := range configTryTimeouts {
		valid = valid || s == t.timeoutSec
	}

	if !valid {
		return "", fmt.Errorf("invalid try timeout %d s (60, 300 or 600)", t.timeoutSec)
	}

	timeout := time.Duration(t.timeoutSec) * time.Second

	snap, err := a.current(ctx)
	if err != nil {
		return "", err
	}

	plan, err := planConfigChange(snap, t.base, t.draft)
	if err != nil {
		return "", err
	}

	if !plan.changed() {
		return "", errConfigUnchanged
	}

	// Asked first so the refusal is ours, and clear: Talos has no revert across a reboot.
	dry, err := a.apply(ctx, plan.data, machineapi.ApplyConfigurationRequest_AUTO, true, 0)
	if err != nil {
		return "", err
	}

	if needsReboot(dry) {
		return "", errConfigNeedsReboot
	}

	t.emit(tryPhaseApplying, "", time.Time{})

	sent := time.Now()
	warning := ""

	if _, err := a.apply(ctx, plan.data, machineapi.ApplyConfigurationRequest_TRY, false, timeout); err != nil {
		// The answer can be lost while the node did apply: a change to its network is the
		// very reason to try one. Only a node showing its previous config has refused.
		if now, readErr := a.current(ctx); readErr == nil && now.redacted == plan.before {
			return "", err
		}

		warning = fmt.Sprintf("the node did not confirm the change (%v): it may hold it, and reverts by itself at the end of the timeout", err)
	}

	deadline := sent.Add(timeout)
	expired := t.after(time.Until(deadline))

	t.emit(tryPhaseTrying, warning, deadline)

	for {
		select {
		case <-ctx.Done():
			return "", errStoppedTry
		case <-expired:
			return confirmRevert(ctx, a, plan, t)
		case command := <-t.commands:
			outcome, err := runTryCommand(ctx, a, command, snap, plan, t)
			if err == nil {
				return outcome, nil
			}

			// The try is still on: say why it failed and let the user ask again.
			t.emit(tryPhaseTrying, err.Error(), deadline)
		}
	}
}

func runTryCommand(ctx context.Context, a configApplier, command string, snap configSnapshot, plan configPlan, t configTry) (string, error) {
	if command == tryCommandKeep {
		t.emit(tryPhaseKeeping, "", time.Time{})

		if _, err := a.apply(ctx, plan.data, machineapi.ApplyConfigurationRequest_AUTO, false, 0); err != nil {
			return "", fmt.Errorf("keeping the change failed: %w", err)
		}

		return tryOutcomeKept, nil
	}

	t.emit(tryPhaseReverting, "", time.Time{})

	if _, err := a.apply(ctx, snap.raw, machineapi.ApplyConfigurationRequest_AUTO, false, 0); err != nil {
		return "", fmt.Errorf("reverting now failed (the node still reverts by itself): %w", err)
	}

	return tryOutcomeReverted, nil
}

// confirmRevert waits for the node to show its previous config again after the timeout.
func confirmRevert(ctx context.Context, a configApplier, plan configPlan, t configTry) (string, error) {
	t.emit(tryPhaseReverting, "", time.Time{})

	var last error

	for range configRevertChecks {
		snap, err := a.current(ctx)
		if err == nil && snap.redacted == plan.before {
			return tryOutcomeReverted, nil
		}

		last = err

		select {
		case <-ctx.Done():
			return "", errStoppedTry
		case <-t.after(configRevertWait):
		}
	}

	if last != nil {
		return "", fmt.Errorf("the try ended but the node could not be asked for its config: %w", last)
	}

	return "", errors.New("the try ended but the node still shows the edited config: check it")
}
