package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

// upgrader is what an upgrade needs from the node (ctx carries the node). Tests run it
// against a fake, never a cluster.
type upgrader interface {
	// legacyUpgrade is MachineService.Upgrade: one call that validates etcd, then cordons and
	// drains, installs and reboots on its own.
	legacyUpgrade(ctx context.Context, image string, stage, force bool) error
	// pullImage is ImageService.Pull into the system containerd.
	pullImage(ctx context.Context, image string) error
	// lifecycleUpgrade is LifecycleService.Upgrade: it installs image to the system disk,
	// streaming the installer's output, and returns its exit code. It does not reboot.
	lifecycleUpgrade(ctx context.Context, image string, progress func(string)) (int32, error)
	reboot(ctx context.Context) error
}

// requestUpgrade asks the node to upgrade. It returns once the node is installing (legacy
// API) or has installed and was told to reboot (lifecycle API); followUpgrade then watches
// it come back.
//
// The legacy MachineService.Upgrade is tried first: Talos serves it up to 1.17 and does the
// whole sequence server-side. When the node answers Unimplemented (Talos 1.18 removes it),
// the LifecycleService path of `talosctl upgrade` on Talos 1.13+ is used instead: pull the
// installer image, LifecycleService.Upgrade, then Reboot. What differs:
//   - etcd is not checked by the node: UpgradePlan's client-side checks (members healthy,
//     quorum kept) are the only guard, so force does not skip them on this path: unforced
//     (the plan's blockers without force) must pass before anything is installed;
//   - the node is not cordoned and drained before it reboots: talosctl does that through
//     the Kubernetes API, which this app does not speak. The reboot stops the pods like any
//     Talos reboot and Kubernetes reschedules them once the node is NotReady;
//   - there is no staged upgrade: stage is refused instead of silently ignored.
func requestUpgrade(ctx context.Context, u upgrader, image string, stage, force bool, unforced func() error, emit func(phase, msg string)) error {
	err := u.legacyUpgrade(ctx, image, stage, force)
	if err == nil {
		emit(phaseInstall, "upgrade accepted: draining and installing")

		return nil
	}

	if !isUnavailableAPI(err) {
		return errors.New("upgrade request failed: " + friendlyError(err))
	}

	if stage {
		return errors.New("this node's Talos version has no staged upgrade: start the upgrade without it")
	}

	if err := unforced(); err != nil {
		return fmt.Errorf("%w (this Talos version does not check etcd itself, so force cannot skip it)", err)
	}

	emit(phaseRequested, noDrainWarning+": pulling "+image)

	if err := u.pullImage(ctx, image); err != nil {
		if isUnavailableAPI(err) {
			return err
		}

		return errors.New("pulling the installer image failed: " + friendlyError(err))
	}

	emit(phaseInstall, "installing (the node is not drained: its pods stop when it reboots)")

	var last string

	code, err := u.lifecycleUpgrade(ctx, image, func(msg string) {
		if msg = strings.TrimSpace(msg); msg != "" {
			last = msg

			emit(phaseInstall, msg)
		}
	})
	if err != nil {
		if isUnavailableAPI(err) {
			return err
		}

		return errors.New("upgrade failed: " + friendlyError(err))
	}

	if code != 0 {
		if last != "" {
			return fmt.Errorf("the installer failed (exit code %d): %s", code, last)
		}

		return fmt.Errorf("the installer failed (exit code %d)", code)
	}

	emit(phaseRebooting, "installed: rebooting into the new version")

	if err := u.reboot(ctx); err != nil {
		return errors.New("the upgrade is installed but the reboot request failed (reboot the node to apply it): " + friendlyError(err))
	}

	return nil
}

// noDrainWarning is shown (plan warning, upgrade progress) when the upgrade goes through the
// LifecycleService.
const noDrainWarning = "this Talos version upgrades without draining the node first"

// legacyUpgradeRemoved is the Talos version expected to drop MachineService.Upgrade
// (remove_deprecated_method = "v1.18" in api/machine/machine.proto).
const legacyUpgradeRemoved = "v1.18"

// talosUpgrader is upgrader on a Talos client.
type talosUpgrader struct{ c *client.Client }

// systemContainerd is where talosctl pulls the installer image (`upgrade --namespace system`).
func systemContainerd() *common.ContainerdInstance {
	return &common.ContainerdInstance{Driver: common.ContainerDriver_CRI, Namespace: common.ContainerdNamespace_NS_SYSTEM}
}

func (t talosUpgrader) legacyUpgrade(ctx context.Context, image string, stage, force bool) error {
	//nolint:staticcheck // deprecated for the LifecycleService, which requestUpgrade falls back to
	_, err := t.c.UpgradeWithOptions(ctx,
		client.WithUpgradeImage(image),
		client.WithUpgradeRebootMode(machineapi.UpgradeRequest_DEFAULT),
		client.WithUpgradeStage(stage),
		client.WithUpgradeForce(force),
	)

	return err
}

func (t talosUpgrader) pullImage(ctx context.Context, image string) error {
	stream, err := t.c.ImageClient.Pull(ctx, &machineapi.ImageServicePullRequest{Containerd: systemContainerd(), ImageRef: image})
	if err != nil {
		return err
	}

	for {
		if _, err := stream.Recv(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}
	}
}

func (t talosUpgrader) lifecycleUpgrade(ctx context.Context, image string, progress func(string)) (int32, error) {
	stream, err := t.c.LifecycleClient.Upgrade(ctx, &machineapi.LifecycleServiceUpgradeRequest{
		Containerd: systemContainerd(),
		Source:     &machineapi.InstallArtifactsSource{ImageName: image},
	})
	if err != nil {
		return 0, err
	}

	var (
		code     int32
		finished bool
	)

	for {
		resp, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			if !finished {
				return 0, errors.New("the node closed the upgrade stream without a result")
			}

			return code, nil
		case err != nil:
			return 0, err
		}

		switch r := resp.GetProgress().GetResponse().(type) {
		case *machineapi.LifecycleServiceInstallProgress_Message:
			progress(r.Message)
		case *machineapi.LifecycleServiceInstallProgress_ExitCode:
			code, finished = r.ExitCode, true
		}
	}
}

func (t talosUpgrader) reboot(ctx context.Context) error {
	return t.c.Reboot(ctx)
}
