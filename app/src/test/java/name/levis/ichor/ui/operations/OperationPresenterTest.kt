package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ArgoSyncRunState
import name.levis.ichor.data.ClusterUpgradeRunState
import name.levis.ichor.data.FluxReconcileRunState
import name.levis.ichor.data.NetPerfRunState
import name.levis.ichor.data.PodSweepRun
import name.levis.ichor.data.RolloutRunState
import name.levis.ichor.data.ConfigMultiRunState
import name.levis.ichor.data.ConfigTryRunState
import name.levis.ichor.data.ImagePullRunState
import name.levis.ichor.data.ImageScanSession
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.model.HetznerSnapshotSpec
import name.levis.ichor.data.K8sUpgradeRunState
import name.levis.ichor.data.MaintenanceRunState
import name.levis.ichor.data.UpgradeRunState
import name.levis.ichor.model.ArgoSyncProgress
import name.levis.ichor.model.ClusterUpgradeProgress
import name.levis.ichor.model.FluxReconcileProgress
import name.levis.ichor.model.KubeRolloutPod
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.NETPERF_LATENCY
import name.levis.ichor.model.NETPERF_PATH_POD
import name.levis.ichor.model.NETPERF_PHASE_TESTING
import name.levis.ichor.model.NETPERF_THROUGHPUT
import name.levis.ichor.model.NetPerfProgress
import name.levis.ichor.model.NetPerfResult
import name.levis.ichor.model.NetPerfSetup
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.model.IMAGESCAN_PHASE_SCANNING
import name.levis.ichor.model.ImagePullNamespace
import name.levis.ichor.model.ImagePullProgress
import name.levis.ichor.model.ImageScanProgress
import name.levis.ichor.model.K8sUpgradeProgress
import name.levis.ichor.model.MaintenanceAction
import name.levis.ichor.model.MultiApplyRun
import name.levis.ichor.model.MultiConfigNodeState
import name.levis.ichor.model.MultiConfigProgress
import name.levis.ichor.model.UpgradeProgress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OperationPresenterTest {

    @Test
    fun aSingleNodeUpgradeSpinsAndCanOnlyStopBeingFollowed() {
        val run = UpgradeRunState("10.0.0.2", "w-1", "v1.10.0", "ghcr.io/siderolabs/installer:v1.11.0", listOf(UpgradeProgress("installing", "pulling the installer")))
        val n = UpgradePresenter.progress(run)
        assertEquals(res(R.string.upgrade_notification_title, "w-1"), n.title)
        assertEquals(raw("pulling the installer"), n.text)
        assertEquals(OperationProgress.INDETERMINATE, n.progress)
        assertEquals(listOf(OperationAction.STOP_FOLLOWING), n.actions)
        // Nothing reported yet: the first phase.
        assertEquals(res(R.string.upgrade_phase_requested), UpgradePresenter.progress(run.copy(events = emptyList())).text)
    }

    @Test
    fun aFailedRunShowsItsError() {
        val run = UpgradeRunState("10.0.0.2", "w-1", "v1.10.0", "img", finished = true, error = "node did not come back")
        assertEquals(OperationResult(res(R.string.upgrade_notification_failed, "w-1"), raw("node did not come back")), UpgradePresenter.result(run))
        val done = run.copy(error = null, newVersion = "v1.11.0")
        assertEquals(res(R.string.upgrade_done_versions, "v1.10.0", "v1.11.0"), UpgradePresenter.result(done).text)
    }

    @Test
    fun aMaintenanceStopsOnceAsked() {
        val run = MaintenanceRunState("10.0.0.2", "w-1", MaintenanceAction.REBOOT)
        assertEquals(listOf(OperationAction.STOP), MaintenancePresenter.progress(run).actions)
        assertEquals(res(R.string.maintenance_phase_cordon), MaintenancePresenter.progress(run).text)
        assertEquals(emptyList<OperationAction>(), MaintenancePresenter.progress(run.copy(stopping = true)).actions)
    }

    @Test
    fun aConfigTryCountsDownWithoutABarAndOffersKeepAndRevert() {
        val presenter = ConfigTryPresenter(time = { "10:42" })
        val trying = ConfigTryRunState("10.0.0.2", "cp-1", ConfigTryState.Running(ConfigTryState.TRYING, deadline = 1_000_000))
        val n = presenter.progress(trying)
        assertNull(n.progress)
        assertEquals(1_000_000L, n.countdownTo)
        assertEquals(listOf(OperationAction.KEEP, OperationAction.REVERT), n.actions)
        assertEquals(OperationText.Joined(listOf(res(R.string.config_try_notification_reverts_at, "10:42"), raw("")), "\n"), n.text)

        val keeping = trying.copy(state = ConfigTryState.Running(ConfigTryState.KEEPING, "keep refused", 1_000_000))
        assertEquals(emptyList<OperationAction>(), presenter.progress(keeping).actions)
        assertEquals(OperationText.Joined(listOf(res(R.string.machine_config_try_keeping), raw("keep refused")), "\n"), presenter.progress(keeping).text)

        assertEquals(res(R.string.config_try_notification_kept, "cp-1"), presenter.result(trying.copy(state = ConfigTryState.Kept)).title)
    }

    @Test
    fun aMultiNodeApplyFillsItsBarNodeByNode() {
        val progress = MultiConfigProgress(
            message = "rebooting",
            index = 1,
            total = 3,
            nodes = listOf(
                MultiConfigNodeState("10.0.0.3", "w-1", MultiConfigNodeState.DONE),
                MultiConfigNodeState("10.0.0.4", "w-2", MultiConfigNodeState.APPLYING),
                MultiConfigNodeState("10.0.0.1", "cp-1"),
            ),
        )
        val run = ConfigMultiRunState("10.0.0.1", "lab", MultiApplyRun(ConfigApplyMode.REBOOT, progress))
        val n = ConfigMultiPresenter.progress(run)
        assertEquals(OperationProgress(1, 3), n.progress)
        assertEquals(
            OperationText.Joined(
                listOf(res(R.string.machine_config_multi_progress, 2, 3), OperationText.Joined(listOf(raw("w-2"), raw("rebooting")), ": ")),
                " · ",
            ),
            n.text,
        )
        assertEquals(listOf(OperationAction.STOP), n.actions)

        val stopped = run.copy(run = run.run.copy(finished = true, stopped = true))
        assertEquals(OperationResult(res(R.string.config_multi_notification_stopped, "lab"), raw("w-1")), ConfigMultiPresenter.result(stopped))
        // Not started yet: a spinner.
        assertEquals(OperationProgress.INDETERMINATE, ConfigMultiPresenter.progress(run.copy(run = MultiApplyRun(ConfigApplyMode.AUTO))).progress)
    }

    @Test
    fun aClusterUpgradePausesResumesAndAborts() {
        val p = ClusterUpgradeProgress(phase = ClusterUpgradeProgress.NODE, index = 1, total = 5, hostname = "w-1", nodePhase = "installing")
        val run = ClusterUpgradeRunState("v1.11.0", "lab", p)
        val n = ClusterUpgradePresenter.progress(run)
        assertEquals(OperationProgress(1, 5), n.progress)
        assertEquals(listOf(OperationAction.PAUSE, OperationAction.ABORT), n.actions)
        assertEquals(res(R.string.cluster_upgrade_step, 2, 5, "w-1", res(R.string.upgrade_phase_installing)), n.text)

        val paused = run.copy(progress = p.copy(phase = ClusterUpgradeProgress.PAUSED))
        assertEquals(listOf(OperationAction.RESUME, OperationAction.ABORT), ClusterUpgradePresenter.progress(paused).actions)
        assertEquals(emptyList<OperationAction>(), ClusterUpgradePresenter.progress(run.copy(aborting = true)).actions)
    }

    @Test
    fun aKubernetesUpgradeCountsWhenTheCoreDoes() {
        val step = K8sUpgradeProgress(phase = K8sUpgradeProgress.CONTROL_PLANE, index = 3, total = 9, hostname = "cp-2", component = "kube-scheduler")
        val run = K8sUpgradeRunState("1.34.1", "lab", dryRun = false, events = listOf(step))
        val n = K8sUpgradePresenter.progress(run)
        assertEquals(OperationProgress(3, 9), n.progress)
        assertEquals(res(R.string.k8s_upgrade_step, 4, 9, "cp-2", "kube-scheduler"), n.text)
        assertEquals(listOf(OperationAction.STOP), n.actions)

        val between = run.copy(events = listOf(K8sUpgradeProgress(phase = K8sUpgradeProgress.PROXY)))
        assertEquals(OperationProgress.INDETERMINATE, K8sUpgradePresenter.progress(between).progress)
        assertEquals(emptyList<OperationAction>(), K8sUpgradePresenter.progress(run.copy(cancelling = true)).actions)
    }

    @Test
    fun anImagePullCountsNodes() {
        val run = ImagePullRunState("ghcr.io/example/app:1.0", ImagePullNamespace.SYSTEM, ImagePullProgress(done = 2, total = 5))
        val n = ImagePullPresenter.progress(run)
        assertEquals(OperationProgress(2, 5), n.progress)
        assertEquals(res(R.string.image_pull_progress, 2, 5), n.text)
        assertEquals(res(R.string.image_pull_stopped), ImagePullPresenter.result(run.copy(finished = true, stopping = true)).title)
    }

    @Test
    fun aJoinCanOnlyStopBeingFollowed() {
        val request = JoinRequest("10.0.0.9", "worker", "worker", "w-9", "/dev/sda", "")
        val run = JoinRunState(request, "lab")
        assertEquals(listOf(OperationAction.STOP_FOLLOWING), JoinPresenter.progress(run).actions)
        assertEquals(res(R.string.join_notification_title, "w-9"), JoinPresenter.progress(run).title)
    }

    @Test
    fun aSnapshotRunIsNotNamedLikeANode() {
        val spec = HetznerSnapshotSpec(arch = "amd64", location = "fsn1")
        val request = JoinRequest("", "", "", "Talos amd64", "", "", snapshot = spec)
        val run = JoinRunState(request, "lab")
        assertEquals(res(R.string.hetzner_snapshot_notification_title), JoinPresenter.progress(run).title)
        assertEquals(res(R.string.hetzner_snapshot_notification_failed), JoinPresenter.failedTitle(run))
        assertEquals(listOf(OperationAction.STOP_FOLLOWING), JoinPresenter.progress(run).actions)
    }

    @Test
    fun anImageScanCountsImagesWhileScanning() {
        val run = ImageScanSession("app-1", "shop", progress = ImageScanProgress(phase = IMAGESCAN_PHASE_SCANNING, step = 2, steps = 4, image = "ghcr.io/example/app:1.0"))
        assertEquals(OperationProgress(1, 4), ImageScanPresenter.progress(run).progress)
        assertEquals(OperationProgress.INDETERMINATE, ImageScanPresenter.progress(run.copy(progress = null)).progress)
        assertEquals(res(R.string.imagescan_stopping), ImageScanPresenter.progress(run.copy(stopping = true)).text)
    }

    @Test
    fun anArgoSyncShowsItsWaveAndResourcesAndCanBeTerminated() {
        val run = ArgoSyncRunState("argocd", "shop", ArgoSyncProgress("Running", wave = 2, waves = 4, done = 12, total = 30))
        val n = ArgoSyncPresenter.progress(run)
        assertEquals(res(R.string.argo_sync_notification_title, "shop"), n.title)
        assertEquals(
            OperationText.Joined(listOf(OperationText.Joined(listOf(res(R.string.argo_sync_wave, 2, 4), res(R.string.argo_sync_resources, 12, 30)), " · "), raw("")), "\n"),
            n.text,
        )
        assertEquals(OperationProgress(12, 30), n.progress)
        assertEquals(listOf(OperationAction.TERMINATE), n.actions)
        // Not picked up by Argo CD yet: a spinner.
        val pending = ArgoSyncPresenter.progress(run.copy(progress = ArgoSyncProgress("Pending")))
        assertEquals(OperationProgress.INDETERMINATE, pending.progress)
        assertEquals(emptyList<OperationAction>(), ArgoSyncPresenter.progress(run.copy(terminating = true)).actions)

        val done = run.copy(result = "Succeeded", finished = true)
        assertEquals(OperationResult(res(R.string.argo_sync_notification_done, "shop"), res(R.string.argo_sync_resources, 12, 30)), ArgoSyncPresenter.result(done))
        assertEquals(res(R.string.argo_sync_notification_terminated, "shop"), ArgoSyncPresenter.result(done.copy(result = "Terminated")).title)
        assertEquals(res(R.string.argo_sync_notification_failed, "shop"), ArgoSyncPresenter.result(done.copy(result = "Failed", error = "hook failed")).title)
    }

    @Test
    fun aFluxReconcileSpinsUntilReadyAndCanStopBeingFollowed() {
        val run = FluxReconcileRunState("Kustomization", "flux-system", "apps", FluxReconcileProgress("reconciling", "applying the manifests"))
        val n = FluxReconcilePresenter.progress(run)
        assertEquals(res(R.string.flux_reconcile_notification_title, "apps"), n.title)
        assertEquals(raw("applying the manifests"), n.text)
        assertEquals(OperationProgress.INDETERMINATE, n.progress)
        assertEquals(listOf(OperationAction.STOP_FOLLOWING), n.actions)
        assertEquals(res(R.string.flux_reconcile_waiting), FluxReconcilePresenter.progress(run.copy(progress = null)).text)

        assertEquals(res(R.string.flux_reconcile_notification_ready, "apps"), FluxReconcilePresenter.result(run.copy(result = "ready", finished = true)).title)
        assertEquals(res(R.string.flux_reconcile_notification_still, "apps"), FluxReconcilePresenter.result(run.copy(result = "still reconciling", finished = true)).title)
        assertEquals(res(R.string.operation_stopped_following, "apps"), FluxReconcilePresenter.result(run.copy(finished = true, stopped = true)).title)
    }

    @Test
    fun aRolloutCountsTheNewPodsReady() {
        val web = KubeWorkload("Deployment", "shop", "web", desired = 5)
        val pods = (1..5).map { KubeRolloutPod("web-$it", updated = it <= 4, healthy = it <= 3) }
        val run = RolloutRunState(web, KubeRolloutStatus(web, pods = pods))
        val n = RolloutPresenter.progress(run)
        assertEquals(res(R.string.rollout_notification_title, "web"), n.title)
        assertEquals(res(R.string.rollout_notification_ready, 3, 5), n.text)
        assertEquals(OperationProgress(3, 5), n.progress)
        assertEquals(listOf(OperationAction.STOP_FOLLOWING), n.actions)
        assertEquals(OperationProgress.INDETERMINATE, RolloutPresenter.progress(run.copy(status = null)).progress)

        val done = run.copy(status = KubeRolloutStatus(web, done = true), finished = true)
        assertEquals(OperationResult(res(R.string.rollout_notification_done, "web"), res(R.string.rollout_notification_ready, 5, 5)), RolloutPresenter.result(done))
        assertEquals(res(R.string.rollout_notification_stalled, "web"), RolloutPresenter.result(done.copy(status = KubeRolloutStatus(web, failed = true))).title)
        assertEquals(res(R.string.operation_stopped_following, "web"), RolloutPresenter.result(run.copy(finished = true, stopped = true)).title)
    }

    @Test
    fun aNetworkTestCountsItsSecondsAndStops() {
        val setup = NetPerfSetup(server = "w-2", client = "w-1", seconds = 10)
        val progress = NetPerfProgress(NETPERF_PHASE_TESTING, path = NETPERF_PATH_POD, test = NETPERF_LATENCY, step = 2, steps = 2, results = listOf(NetPerfResult(NETPERF_PATH_POD, NETPERF_THROUGHPUT)))
        val run = NetPerfRunState("lab-plain", setup, progress)
        val n = NetPerfPresenter.progress(run)
        assertEquals(res(R.string.netperf_notification_title, "w-1", "w-2"), n.title)
        assertEquals(OperationProgress(10, 20), n.progress)
        val phase = res(R.string.netperf_phase_testing, res(R.string.netperf_path_pod), res(R.string.netperf_test_latency), 2, 2)
        assertEquals(OperationText.Joined(listOf(phase, res(R.string.netperf_notification_seconds, 10, 20)), " · "), n.text)
        assertEquals(listOf(OperationAction.STOP), n.actions)
        assertEquals(emptyList<OperationAction>(), NetPerfPresenter.progress(run.copy(stopping = true)).actions)

        assertEquals(res(R.string.netperf_notification_stopped), NetPerfPresenter.result(run.copy(finished = true, stopping = true)).title)
        assertEquals(res(R.string.netperf_notification_failed), NetPerfPresenter.result(run.copy(finished = true, error = "no route")).title)
    }

    @Test
    fun aStepPastTheEndStaysOnTheBar() {
        assertEquals(OperationProgress(5, 5), OperationProgress.step(7, 5))
        assertEquals(OperationProgress.INDETERMINATE, OperationProgress.step(1, 0))
    }

    @Test
    fun theNotificationCountsThePodsAndOffersStop() {
        val run = PodSweepRun("admin@test", "web", total = 140, done = 12)
        val n = PodSweepPresenter.progress(run)
        assertEquals(res(R.string.pod_sweep_notification_title, raw("web")), n.title)
        assertEquals(res(R.string.pod_sweep_progress, "12", "140"), n.text)
        assertEquals(OperationProgress(12, 140), n.progress)
        assertEquals(listOf(OperationAction.STOP), n.actions)

        val stopping = PodSweepPresenter.progress(run.copy(stopping = true))
        assertEquals(res(R.string.pod_sweep_stopping), stopping.text)
        assertTrue(stopping.actions.isEmpty())

        // Every namespace: worded, not "".
        assertEquals(res(R.string.pod_sweep_notification_title, res(R.string.pod_sweep_every_namespace)), PodSweepPresenter.progress(run.copy(namespace = "")).title)
    }

    @Test
    fun theResultSaysHowManyWentAndHowManyCouldNot() {
        val ended = PodSweepRun("admin@test", "web", total = 3, done = 3, deleted = 3, running = false)
        assertEquals(res(R.string.pod_sweep_done, raw("web")), PodSweepPresenter.result(ended).title)
        assertEquals(res(R.string.pod_sweep_result, "3"), PodSweepPresenter.result(ended).text)

        val partial = ended.copy(deleted = 2, failedCount = 1)
        assertEquals(res(R.string.pod_sweep_result_failed, "2", "1"), PodSweepPresenter.result(partial).text)

        assertEquals(res(R.string.pod_sweep_stopped, raw("web")), PodSweepPresenter.result(ended.copy(stopped = true)).title)

        val failed = PodSweepPresenter.result(ended.copy(error = "forbidden"))
        assertEquals(res(R.string.pod_sweep_failed, raw("web")), failed.title)
        assertEquals(raw("forbidden"), failed.text)
    }
}
