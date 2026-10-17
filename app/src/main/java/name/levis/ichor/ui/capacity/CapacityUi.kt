package name.levis.ichor.ui.capacity

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CapacityNode
import name.levis.ichor.model.CapacityWorkload
import name.levis.ichor.model.KubeCapacity
import name.levis.ichor.model.QosClass
import name.levis.ichor.ui.components.MeterRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors
import kotlin.math.roundToInt

/** A node's memory bars turn amber from 80 %, later than the usual 75 %. */
private const val NODE_WARN_AT = 0.8f

/** The cluster's free room, the N+1 verdict, what is missing to read, and "What if a node goes down". */
@Composable
fun CapacityHeader(capacity: KubeCapacity, onWhatIf: () -> Unit) {
    val colors = LocalStatusColors.current
    val t = capacity.totals
    val n1 = capacity.nPlusOne
    Column(Modifier.padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        StatusPill(
            when {
                n1.ok -> stringResource(R.string.capacity_n_plus_one_ok)
                n1.shortEphemeral > 0 -> stringResource(R.string.capacity_n_plus_one_short_disk, n1.node, n1.pendingPods.toString())
                else -> stringResource(R.string.capacity_n_plus_one_short, n1.node, n1.pendingPods.toString())
            },
            if (n1.ok) colors.ok else colors.warn,
        )
        Text(stringResource(R.string.capacity_free_cpu, cpuText(t.cpuFree), cpuText(t.cpuAllocatable)), style = MaterialTheme.typography.bodyMedium)
        Text(stringResource(R.string.capacity_free_memory, memoryText(t.memoryFree), memoryText(t.memoryAllocatable)), style = MaterialTheme.typography.bodyMedium)
        Text(stringResource(R.string.capacity_free_pods, t.podsFree.toString(), t.podCapacity.toString()), style = MaterialTheme.typography.bodyMedium)
        if (t.ephemeralAllocatable > 0) {
            Text(stringResource(R.string.capacity_free_ephemeral, memoryText(t.ephemeralFree), memoryText(t.ephemeralAllocatable)), style = MaterialTheme.typography.bodyMedium)
        }
        val fullest = capacity.fullestDiskNode
        when {
            capacity.diskMeasured && fullest != null -> Text(
                stringResource(R.string.capacity_disk_fullest, memoryText(fullest.diskAvailable), fullest.name),
                style = MaterialTheme.typography.bodyMedium,
            )
            capacity.diskForbidden -> MutedText(stringResource(R.string.capacity_disk_forbidden))
            else -> MutedText(stringResource(R.string.capacity_disk_unmeasured))
        }
        when {
            capacity.metricsForbidden -> MutedText(stringResource(R.string.capacity_metrics_forbidden))
            !capacity.metricsAvailable -> MutedText(stringResource(R.string.capacity_metrics_unavailable))
        }
        if (!capacity.priorityClassesRead) MutedText(stringResource(R.string.capacity_classes_unread))
        MutedText(stringResource(R.string.capacity_intro))
        if (t.schedulableNodes > 1) OutlinedButton(onClick = onWhatIf) { Text(stringResource(R.string.capacity_what_if)) }
    }
}

/**
 * One node: its memory requests and usage against allocatable as two bars (usage only with
 * metrics-server), its CPU and free room in words; tapping it opens what losing it costs.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun CapacityNodeRow(n: CapacityNode, metrics: Boolean, onOpen: () -> Unit) {
    val colors = LocalStatusColors.current
    val requests = n.memoryPercent.roundToInt().toString()
    val usage = (n.memoryUsageFraction * 100).roundToInt().toString()
    val bars = if (metrics) {
        stringResource(R.string.capacity_node_bars, n.name, requests, usage)
    } else {
        stringResource(R.string.capacity_node_bars_no_usage, n.name, requests)
    }
    OutlinedCard(Modifier.fillMaxWidth()) {
        Column(
            Modifier.fillMaxWidth()
                .clickable(enabled = n.schedulable, role = Role.Button, onClickLabel = stringResource(R.string.capacity_node_open), onClick = onOpen)
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            // The name keeps its own line: only the overcommitted tag sits beside it, so role and
            // disk tags (up to five roles on a real node) wrap below instead of squeezing it away.
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(n.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                if (n.overcommitted) Tag(stringResource(R.string.capacity_overcommitted), colors.bad)
            }
            if (n.diskPressure || n.diskLow || n.roles.isNotEmpty()) {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    if (n.diskPressure) {
                        Tag(stringResource(R.string.capacity_node_disk_pressure), colors.bad)
                    } else if (n.diskLow) {
                        Tag(stringResource(R.string.capacity_node_disk_low), colors.warn)
                    }
                    n.roles.forEach { Tag(it, MaterialTheme.colorScheme.primary) }
                }
            }
            Column(Modifier.clearAndSetSemantics { contentDescription = bars }, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                MeterRow(stringResource(R.string.capacity_requests), (n.memoryPercent / 100).toFloat().coerceIn(0f, 1f), "$requests%", warnAt = NODE_WARN_AT)
                if (metrics) MeterRow(stringResource(R.string.capacity_usage), n.memoryUsageFraction, "$usage%", warnAt = NODE_WARN_AT)
            }
            MutedText(stringResource(R.string.capacity_node_free, cpuText(n.cpuFree), memoryText(n.memoryFree), n.podsFree.toString()), maxLines = 2)
            if (n.diskMeasured && n.diskCapacity > 0) {
                MutedText(stringResource(R.string.capacity_node_disk, memoryText(n.diskAvailable), memoryText(n.diskCapacity)), maxLines = 1)
            }
        }
    }
}

/** One workload: what one replica asks, then its priority class, priority, QoS, risk and how many more fit. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun CapacityWorkloadRow(w: CapacityWorkload) {
    val colors = LocalStatusColors.current
    OutlinedCard(Modifier.fillMaxWidth()) {
        Column(Modifier.fillMaxWidth().padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("${w.kind} ${w.namespace}/${w.name}", style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val asks = stringResource(R.string.capacity_workload_line, w.replicas.toString(), cpuText(w.cpuRequest), memoryText(w.memoryRequest))
            val ephemeral = if (w.ephemeralRequest > 0) stringResource(R.string.capacity_workload_ephemeral, memoryText(w.ephemeralRequest)) else null
            MutedText(
                if (ephemeral == null) asks else "$asks, $ephemeral",
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (w.noPriorityClass) {
                    Tag(stringResource(R.string.capacity_no_class), colors.warn)
                } else {
                    Tag(w.priorityClassName, MaterialTheme.colorScheme.primary, mono = true)
                }
                Tag(stringResource(R.string.capacity_priority, w.priority.toString()), colors.muted)
                if (w.qosClass != QosClass.UNKNOWN) Tag(w.qosClass.label(), colors.muted)
                Tag(riskText(w), w.riskLevel.color())
                if (!w.moreFitNA) Tag(stringResource(R.string.capacity_more_fit, w.moreFit.toString()), if (w.moreFit > 0) colors.ok else colors.warn)
            }
        }
    }
}
