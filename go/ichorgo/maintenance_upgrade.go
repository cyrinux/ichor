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

// StartNodeMaintenanceUpgrade is StartNodeMaintenance with the Talos upgrade to image (see
// StartUpgrade) as the action: cordon, drain, upgrade, wait until the node runs the new
// version and its Kubernetes node is Ready, uncordon. It is meant for the nodes whose upgrade
// does not drain them (NodeMaintenancePlan's upgradeDrainable). The upgrade plan's blockers
// are refused (force skips the etcd ones, as for StartUpgrade), and its acknowledgments and
// the version risk unless acknowledged. The run takes the cluster upgrade lock once, for
// the upgrade to image. Needs os:admin.
func StartNodeMaintenanceUpgrade(configYAML, contextName, kubeServer, node, image string, includeBare, acknowledged, force bool, listener MaintenanceListener) *MaintenanceRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)
	image = strings.TrimSpace(image)

	listener = maskedMaintenanceListener{listener}

	m := maintenance{
		kube:         kubeTarget{configYAML, contextName, kubeServer},
		node:         node,
		action:       maintenanceUpgrade,
		includeBare:  includeBare,
		acknowledged: acknowledged,
		listener:     listener,
		image:        image,
		force:        force,
	}

	return runMaintenance(listener, func(ctx context.Context) error {
		return recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "maintenance-upgrade", Node: node, Params: fmt.Sprintf("image=%s include-bare=%t force=%t", image, includeBare, force)}
		}, func() error { return m.run(ctx) })
	})
}

// lockRequest is the cluster upgrade lock the run takes, and when a held one may be taken
// over: an upgrade's lock says its versions and may replace a settled one, like StartUpgrade;
// another maintenance never takes over a lock someone else holds (it frees itself when that
// run ends or expires).
func (m maintenance) lockRequest(plan maintenancePlan) (upgradeLockRequest, func(*upgradeLockInfo) bool) {
	request := upgradeLockRequest{holder: newLockHolder(), node: m.node, hostname: plan.Hostname, to: maintenanceLockTo}

	if m.action != maintenanceUpgrade {
		return request, func(*upgradeLockInfo) bool { return false }
	}

	up := plan.upgrade
	_, tag := splitImageRef(m.image)
	request.from, request.to = up.CurrentVersion, tag

	return request, func(info *upgradeLockInfo) bool {
		return lockSettled(info, append([]planPeer{up.target}, up.peers...))
	}
}

// upgradeChecks refuses an invalid image and what StartUpgrade refuses.
func (m maintenance) upgradeChecks(plan maintenancePlan) error {
	repo, tag := splitImageRef(m.image)
	if repo == "" || tag == "" && !strings.Contains(m.image, "@") {
		return fmt.Errorf("invalid installer image %q", m.image)
	}

	if err := upgradeRefusal(plan.upgrade, m.force); err != nil {
		return err
	}

	return acknowledgmentRefusal(plan.upgrade, tag, m.acknowledged)
}

// upgradeSteps are the steps of the upgrade action after the drain; tests replace them.
type upgradeSteps struct {
	// recheck runs the upgrade's checks again: the drain may have taken a while.
	recheck func(ctx context.Context) error
	// request asks the node to upgrade, follow waits until it runs the new version.
	request func(ctx context.Context, emit func(phase, msg string)) error
	follow  func(ctx context.Context, emit func(phase, msg string)) error
	// back waits until its Kubernetes node is Ready again.
	back func(ctx context.Context, emit func(string)) error
}

// drainThenUpgrade cordons and drains the node, upgrades it, waits for it, then uncordons
// it. The upgrade's phases are reported as phaseUpgrade; any failure leaves it cordoned.
func (m maintenance) drainThenUpgrade(ctx context.Context, k *kubeClient, plan maintenancePlan, u upgradeSteps) error {
	if _, err := cordonAndDrain(ctx, k, plan.KubeNode, m.includeBare, m.emit); err != nil {
		return err
	}

	if err := u.recheck(ctx); err != nil {
		return stoppedCordoned(plan.KubeNode, err)
	}

	if ctx.Err() != nil {
		return stoppedCordoned(plan.KubeNode, errors.New("cancelled before the upgrade was requested"))
	}

	forward := func(phase, msg string) { m.emit(phaseUpgrade, phase+": "+msg, nil) }

	if err := u.request(ctx, forward); err != nil {
		return stoppedCordoned(plan.KubeNode, err)
	}

	if err := u.follow(ctx, forward); err != nil {
		return stoppedCordoned(plan.KubeNode, err)
	}

	if err := u.back(ctx, func(msg string) { m.emit(phaseBack, msg, nil) }); err != nil {
		return stoppedCordoned(plan.KubeNode, err)
	}

	return m.uncordon(ctx, k, plan)
}

// talosUpgradeSteps are the upgrade steps on the node, through s, with the lock this run holds.
func (m maintenance) talosUpgradeSteps(s *session, k *kubeClient, lock *upgradeLock, plan maintenancePlan) upgradeSteps {
	up := plan.upgrade
	_, tag := splitImageRef(m.image)

	var requestedAt time.Time

	return upgradeSteps{
		recheck: func(ctx context.Context) error {
			if err := lock.stillHeld(ctx, k); err != nil {
				return err
			}

			planCtx, cancel := context.WithTimeout(ctx, planTimeout)
			defer cancel()

			now := gatherPlan(planCtx, s, m.node, kubeTarget{})
			if err := upgradeRefusal(now, m.force); err != nil {
				return err
			}

			for _, a := range now.Acknowledge {
				if !slices.Contains(up.Acknowledge, a) {
					return errors.New("not upgrading, this needs confirming first: " + a)
				}
			}

			return nil
		},
		request: func(_ context.Context, emit func(phase, msg string)) error {
			requestedAt = time.Now()

			// The request is not tied to Cancel: once sent, it completes.
			reqCtx, cancel := context.WithTimeout(context.Background(), upgradeRequestTimeout)
			defer cancel()

			// The node is about to change version: what the session cached about it is void.
			defer func() {
				s.versions.Delete(m.node)
				s.definitions.Delete(m.node)
			}()

			err := requestUpgrade(client.WithNode(reqCtx, m.node), talosUpgrader{s.client}, m.image, false, m.force, true,
				func() error { return upgradeRefusal(up, false) }, emit)
			if err != nil && isUnavailableAPI(err) {
				return errors.New("upgrade: " + s.friendly(m.node, err))
			}

			return err
		},
		follow: func(ctx context.Context, emit func(phase, msg string)) error {
			tracker := &upgradeTracker{oldVersion: up.CurrentVersion, reinstall: tag != "" && sameVersion(tag, up.CurrentVersion)}

			followCtx, cancel := context.WithTimeout(ctx, upgradeTimeout)
			defer cancel()

			_, err := followUpgrade(followCtx, tracker,
				func(ctx context.Context) upgradeObservation { return observeNode(ctx, s.client, m.node) },
				emit, upgradePollInterval, time.Now)

			return err
		},
		back: func(ctx context.Context, emit func(string)) error {
			backCtx, cancel := context.WithTimeout(ctx, backTimeout)
			defer cancel()

			// The upgrade was followed through its reboot: only Kubernetes is left to see it Ready.
			err := waitBackFrom(backCtx, true, func(ctx context.Context) backObservation {
				return observeBack(ctx, s.client, k, m.node, plan.KubeNode, requestedAt)
			}, emit, upgradePollInterval)
			if err != nil && ctx.Err() == nil && errors.Is(backCtx.Err(), context.DeadlineExceeded) {
				err = fmt.Errorf("the node is not back and Ready within %s", backTimeout)
			}

			return err
		},
	}
}
