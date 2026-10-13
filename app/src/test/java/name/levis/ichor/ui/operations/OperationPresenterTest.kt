package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ClusterUpgradeRunState
import name.levis.ichor.data.ConfigMultiRunState
import name.levis.ichor.data.ConfigTryRunState
import name.levis.ichor.data.ImagePullRunState
import name.levis.ichor.data.ImageScanSession
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.data.K8sUpgradeRunState
import name.levis.ichor.data.MaintenanceRunState
import name.levis.ichor.data.UpgradeRunState
import name.levis.ichor.model.ClusterUpgradeProgress
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

        val stopped = run.copy(run = run.run.copy(finished = true), stopping = true)
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
    fun anImageScanCountsImagesWhileScanning() {
        val run = ImageScanSession("app-1", "shop", progress = ImageScanProgress(phase = IMAGESCAN_PHASE_SCANNING, step = 2, steps = 4, image = "ghcr.io/example/app:1.0"))
        assertEquals(OperationProgress(1, 4), ImageScanPresenter.progress(run).progress)
        assertEquals(OperationProgress.INDETERMINATE, ImageScanPresenter.progress(run.copy(progress = null)).progress)
        assertEquals(res(R.string.imagescan_stopping), ImageScanPresenter.progress(run.copy(stopping = true)).text)
    }

    @Test
    fun aStepPastTheEndStaysOnTheBar() {
        assertEquals(OperationProgress(5, 5), OperationProgress.step(7, 5))
        assertEquals(OperationProgress.INDETERMINATE, OperationProgress.step(1, 0))
    }
}
