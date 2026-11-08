package name.levis.ichor.ui.operations

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.FollowedRun

/** One operation as the notifications see it: running (and how far) or ended (and how). */
data class OperationState(
    val kind: OperationKind,
    val running: Boolean,
    val progress: OperationNotification?,
    val result: OperationResult?,
)

/** A kind of run, its manager's state and what its notification's buttons do. */
class FollowedOperation<T : FollowedRun>(
    val kind: OperationKind,
    private val current: StateFlow<T?>,
    private val presenter: OperationPresenter<T>,
    private val control: (OperationAction) -> Unit,
) {
    val states: Flow<OperationState?> get() = current.map(::stateOf)

    fun state(): OperationState? = stateOf(current.value)

    fun act(action: OperationAction) = control(action)

    private fun stateOf(run: T?): OperationState? = run?.let {
        if (it.running) OperationState(kind, true, presenter.progress(it), null) else OperationState(kind, false, null, presenter.result(it))
    }
}

/** Every run the app follows app-wide, in [OperationKind] order. */
fun TalosApp.followedOperations(): List<FollowedOperation<*>> = listOf(
    FollowedOperation(OperationKind.UPGRADE, upgradeManager.current, UpgradePresenter) { action ->
        if (action == OperationAction.STOP_FOLLOWING) upgradeManager.stopFollowing()
    },
    FollowedOperation(OperationKind.MAINTENANCE, maintenanceManager.current, MaintenancePresenter) { action ->
        if (action == OperationAction.STOP) maintenanceManager.stop()
    },
    FollowedOperation(OperationKind.CONFIG_TRY, configTryManager.current, ConfigTryPresenter()) { action ->
        when (action) {
            OperationAction.KEEP -> configTryManager.keep()
            OperationAction.REVERT -> configTryManager.revert()
            else -> Unit
        }
    },
    FollowedOperation(OperationKind.CONFIG_MULTI, configMultiManager.current, ConfigMultiPresenter) { action ->
        if (action == OperationAction.STOP) configMultiManager.stop()
    },
    FollowedOperation(OperationKind.CLUSTER_UPGRADE, clusterUpgradeManager.current, ClusterUpgradePresenter) { action ->
        when (action) {
            OperationAction.PAUSE -> clusterUpgradeManager.pause()
            OperationAction.RESUME -> clusterUpgradeManager.resume()
            OperationAction.ABORT -> clusterUpgradeManager.abort()
            else -> Unit
        }
    },
    FollowedOperation(OperationKind.K8S_UPGRADE, k8sUpgradeManager.current, K8sUpgradePresenter) { action ->
        if (action == OperationAction.STOP) k8sUpgradeManager.cancel()
    },
    FollowedOperation(OperationKind.IMAGE_PULL, imagePullManager.current, ImagePullPresenter) { action ->
        if (action == OperationAction.STOP) imagePullManager.stop()
    },
    FollowedOperation(OperationKind.JOIN, joinManager.current, JoinPresenter) { action ->
        if (action == OperationAction.STOP_FOLLOWING) joinManager.stop()
    },
    FollowedOperation(OperationKind.IMAGE_SCAN, imageScanRepository.session, ImageScanPresenter) { action ->
        if (action == OperationAction.STOP) imageScanRepository.stop()
    },
    FollowedOperation(OperationKind.ARGO_SYNC, gitOpsOperations.argoSync, ArgoSyncPresenter) { action ->
        if (action == OperationAction.TERMINATE) gitOpsOperations.terminate()
    },
    FollowedOperation(OperationKind.FLUX_RECONCILE, gitOpsOperations.fluxReconcile, FluxReconcilePresenter) { action ->
        if (action == OperationAction.STOP_FOLLOWING) gitOpsOperations.stopFollowingFlux()
    },
    FollowedOperation(OperationKind.ROLLOUT, rolloutManager.current, RolloutPresenter) { action ->
        if (action == OperationAction.STOP_FOLLOWING) rolloutManager.stop()
    },
    FollowedOperation(OperationKind.NETPERF, netPerfManager.current, NetPerfPresenter) { action ->
        if (action == OperationAction.STOP) netPerfManager.stop()
    },
    FollowedOperation(OperationKind.RUNBOOK, runbookManager.current, RunbookPresenter) { action ->
        if (action == OperationAction.STOP) runbookManager.stop()
    },
    FollowedOperation(OperationKind.POD_SWEEP, podSweepManager.current, PodSweepPresenter) { action ->
        if (action == OperationAction.STOP) podSweepManager.stop()
    },
    FollowedOperation(OperationKind.CA_ROTATION, caRotationManager.current, CARotationPresenter) { action ->
        if (action == OperationAction.STOP) caRotationManager.stop()
    },
)
