package name.levis.ichor.ui.upgrade

import android.content.Context
import name.levis.ichor.R
import name.levis.ichor.model.ClusterUpgradeProgress
import name.levis.ichor.model.K8sUpgradeProgress
import name.levis.ichor.model.UpgradePhase
import name.levis.ichor.ui.operations.OperationText
import name.levis.ichor.ui.operations.raw
import name.levis.ichor.ui.operations.res
import name.levis.ichor.ui.operations.resolve

/** What the roll does now, in words: the node's own upgrade phase, the gate, or why it waits. */
fun phaseLabel(p: ClusterUpgradeProgress): OperationText = when (p.phase) {
    ClusterUpgradeProgress.GATE -> res(R.string.cluster_upgrade_gate)
    ClusterUpgradeProgress.PAUSED -> res(R.string.cluster_upgrade_paused)
    ClusterUpgradeProgress.DONE -> res(R.string.upgrade_phase_done)
    else -> UpgradePhase.of(p.nodePhase)?.let { res(it.label) }
        ?: if (p.nodePhase.isEmpty()) res(R.string.upgrade_phase_requested) else raw(p.nodePhase)
}

fun phaseText(res: Context, p: ClusterUpgradeProgress): String = phaseLabel(p).resolve(res)

/** "Kubernetes upgrade 4/9: cp-2 scheduler", or what it does between nodes. */
fun k8sStepLabel(p: K8sUpgradeProgress): OperationText = when {
    p.phase == K8sUpgradeProgress.DONE -> res(R.string.upgrade_phase_done)
    p.phase == K8sUpgradeProgress.PROXY -> res(R.string.k8s_upgrade_proxy)
    p.total > 0 -> res(R.string.k8s_upgrade_step, (p.index + 1).coerceAtMost(p.total), p.total, p.name, p.component.ifEmpty { p.phase })
    else -> raw(p.message)
}

fun k8sStepText(res: Context, p: K8sUpgradeProgress): String = k8sStepLabel(p).resolve(res)
