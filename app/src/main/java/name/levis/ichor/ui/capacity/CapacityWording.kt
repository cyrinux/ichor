package name.levis.ichor.ui.capacity

import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.CapacityRisk
import name.levis.ichor.model.CapacityWorkload
import name.levis.ichor.model.QosClass
import name.levis.ichor.model.formatCpu
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** A workload's risk in words: why it may be killed, and by whom. */
@Composable
fun riskText(w: CapacityWorkload): String = when (w.riskLevel) {
    CapacityRisk.OOM -> if (w.oomKills > 0) {
        stringResource(R.string.capacity_risk_oom_kills, w.oomKills.toString())
    } else {
        stringResource(R.string.capacity_risk_oom_near)
    }
    CapacityRisk.DISK_NEAR -> stringResource(R.string.capacity_risk_disk_near)
    CapacityRisk.EVICT_FIRST -> stringResource(R.string.capacity_risk_evict_first)
    CapacityRisk.UNBOUNDED -> stringResource(R.string.capacity_risk_unbounded)
    CapacityRisk.OK -> stringResource(R.string.capacity_risk_ok)
}

@Composable
fun CapacityRisk.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        CapacityRisk.OOM -> colors.bad
        CapacityRisk.DISK_NEAR, CapacityRisk.EVICT_FIRST, CapacityRisk.UNBOUNDED -> colors.warn
        CapacityRisk.OK -> colors.ok
    }
}

/** The QoS class as Kubernetes names it; "" when the core did not say. */
fun QosClass.label(): String = when (this) {
    QosClass.BEST_EFFORT -> "BestEffort"
    QosClass.BURSTABLE -> "Burstable"
    QosClass.GUARANTEED -> "Guaranteed"
    QosClass.UNKNOWN -> ""
}

/** Why a pod of the node-loss plan stays Pending. */
@Composable
fun pendingReasonText(reason: String): String = when (reason) {
    "cpu" -> stringResource(R.string.capacity_loss_pending_cpu)
    "memory" -> stringResource(R.string.capacity_loss_pending_memory)
    else -> stringResource(R.string.capacity_loss_pending_pods)
}

/** CPU as kubectl prints it, memory in binary units. */
fun cpuText(cores: Double): String = formatCpu(cores)

fun memoryText(bytes: Double): String = formatBytes(bytes.toLong())
