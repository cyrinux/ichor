package name.levis.ichor.ui.capacity

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CapacityMajority
import name.levis.ichor.model.CapacityNode
import name.levis.ichor.model.CapacityRisk
import name.levis.ichor.model.CapacityWorkload
import name.levis.ichor.model.KubeCapacity
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.freeFraction
import name.levis.ichor.model.nodeRolesLine
import name.levis.ichor.model.traitsBeside
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.components.MeterRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.StatTile
import name.levis.ichor.ui.components.StatTileRow
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.theme.LocalStatusColors
import kotlin.math.roundToInt

/** A node's memory bars turn amber from 80 %, later than the usual 75 %. */
private const val NODE_WARN_AT = 0.8f

/** A trailing pill wraps rather than squeeze the row's title away. */
private val PILL_MAX_WIDTH = 160.dp

/**
 * The N+1 verdict, the cluster's free room as tiles (CPU, memory, pod slots, and ephemeral
 * storage when the nodes report it: then two rows of two), what is missing to read, and
 * "What if a node goes down".
 */
@Composable
fun CapacityHeader(capacity: KubeCapacity, onWhatIf: () -> Unit) {
    val colors = LocalStatusColors.current
    val t = capacity.totals
    val n1 = capacity.nPlusOne
    Column(Modifier.padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            StatusPill(
                when {
                    n1.ok -> stringResource(R.string.capacity_n_plus_one_ok)
                    n1.shortEphemeral > 0 -> stringResource(R.string.capacity_n_plus_one_short_disk, n1.node, n1.pendingPods.toString())
                    else -> stringResource(R.string.capacity_n_plus_one_short, n1.node, n1.pendingPods.toString())
                },
                if (n1.ok) colors.ok else colors.warn,
                Modifier.weight(1f, fill = false),
            )
            InfoHint(stringResource(R.string.capacity_title), stringResource(R.string.capacity_intro))
        }
        val cpu: @Composable (Modifier) -> Unit = {
            FreeTile(cpuText(t.cpuFree), cpuText(t.cpuAllocatable), stringResource(R.string.capacity_tile_cpu_free), freeFraction(t.cpuRequests, t.cpuAllocatable), it)
        }
        val memory: @Composable (Modifier) -> Unit = {
            FreeTile(memoryText(t.memoryFree), memoryText(t.memoryAllocatable), stringResource(R.string.capacity_tile_memory_free), freeFraction(t.memoryRequests, t.memoryAllocatable), it)
        }
        val pods: @Composable (Modifier) -> Unit = {
            FreeTile(t.podsFree.toString(), t.podCapacity.toString(), stringResource(R.string.capacity_tile_pods), freeFraction(t.pods.toDouble(), t.podCapacity.toDouble()), it)
        }
        if (t.ephemeralAllocatable > 0) {
            val ephemeral: @Composable (Modifier) -> Unit = {
                FreeTile(
                    memoryText(t.ephemeralFree),
                    memoryText(t.ephemeralAllocatable),
                    stringResource(R.string.capacity_tile_ephemeral_free),
                    freeFraction(t.ephemeralRequests, t.ephemeralAllocatable),
                    it,
                )
            }
            StatTileRow(Modifier, cpu, memory)
            StatTileRow(Modifier, pods, ephemeral)
        } else {
            StatTileRow(Modifier, cpu, memory, pods)
        }
        CapacityNotes(capacity)
        if (t.schedulableNodes > 1) OutlinedButton(onClick = onWhatIf) { Text(stringResource(R.string.capacity_what_if)) }
    }
}

/** "31.2 of 60.6" over "CPU free", with the requested share as a bar. */
@Composable
private fun FreeTile(free: String, of: String, label: String, used: Float, modifier: Modifier) {
    StatTile(null, stringResource(R.string.capacity_tile_of, free, of), label, modifier) {
        UsageBar(used, Modifier.padding(top = 4.dp))
    }
}

/** The disk, metrics and priority-class notes, one muted line each. */
@Composable
private fun CapacityNotes(capacity: KubeCapacity) {
    val fullest = capacity.fullestDiskNode
    when {
        capacity.diskMeasured && fullest != null ->
            MutedText(stringResource(R.string.capacity_disk_fullest, memoryText(fullest.diskAvailable), fullest.name))
        capacity.diskForbidden -> MutedText(stringResource(R.string.capacity_disk_forbidden))
        else -> MutedText(stringResource(R.string.capacity_disk_unmeasured))
    }
    when {
        capacity.metricsForbidden -> MutedText(stringResource(R.string.capacity_metrics_forbidden))
        !capacity.metricsAvailable -> MutedText(stringResource(R.string.capacity_metrics_unavailable))
    }
    if (!capacity.priorityClassesRead) MutedText(stringResource(R.string.capacity_classes_unread))
}

/**
 * One node: its name, its roles as text, its memory requests and usage against allocatable as
 * two meters (usage only with metrics-server), its free room and disk; tapping it opens what
 * losing it costs. One pill at most: overcommitted, else DiskPressure, else disk low.
 */
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
    val pill = when {
        n.overcommitted -> stringResource(R.string.capacity_overcommitted) to colors.bad
        n.diskPressure -> stringResource(R.string.capacity_node_disk_pressure) to colors.bad
        n.diskLow -> stringResource(R.string.capacity_node_disk_low) to colors.warn
        else -> null
    }
    ResourceRow(
        health = null,
        title = n.name,
        subtitle = nodeRolesLine(n.roles),
        modifier = Modifier.clickable(
            enabled = n.schedulable,
            role = Role.Button,
            onClickLabel = stringResource(R.string.capacity_node_open),
            onClick = onOpen,
        ),
        trailing = { pill?.let { (label, color) -> StatusPill(label, color, Modifier.widthIn(max = PILL_MAX_WIDTH)) } },
    ) {
        Column(Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
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

/**
 * One workload: what its replicas ask and how many more fit, its risk as the only pill (none
 * when it is fine), then its kind and the priority class, priority and QoS class that differ
 * from [majority].
 */
@Composable
fun CapacityWorkloadRow(w: CapacityWorkload, majority: CapacityMajority) {
    val parts = buildList {
        add(stringResource(R.string.capacity_workload_replicas, w.replicas.toString(), cpuText(w.cpuRequest)))
        add(memoryText(w.memoryRequest))
        if (w.ephemeralRequest > 0) add(stringResource(R.string.capacity_workload_ephemeral, memoryText(w.ephemeralRequest)))
        if (!w.moreFitNA) add(stringResource(R.string.capacity_more_fit, w.moreFit.toString()))
        if (!w.atRisk) add(stringResource(R.string.capacity_risk_ok))
    }
    val risk = if (w.atRisk) riskText(w) to w.riskLevel.color() else null
    ResourceRow(
        health = w.health(),
        title = "${w.namespace}/${w.name}",
        subtitle = parts.joinToString(" · "),
        trailing = { risk?.let { (label, color) -> StatusPill(label, color, Modifier.widthIn(max = PILL_MAX_WIDTH)) } },
    ) {
        WorkloadTraitsLine(w, majority)
    }
}

/** The kind as a short tag, then "platform-optional · priority 10 · Burstable" without the majority values. */
@Composable
private fun WorkloadTraitsLine(w: CapacityWorkload, majority: CapacityMajority) {
    val shown = w.traitsBeside(majority)
    val traits = buildList {
        if (shown.priorityClass) add(if (w.noPriorityClass) stringResource(R.string.capacity_no_class) else w.priorityClassName)
        if (shown.priority) add(stringResource(R.string.capacity_priority, w.priority.toString()))
        if (shown.qos) add(w.qosClass.label())
    }
    Row(Modifier.padding(top = 2.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Tag(kindShort(w.kind))
        if (traits.isNotEmpty()) MutedText(traits.joinToString(" · "), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** OOM is bad, any other risk or a missing priority class a warning, nothing otherwise. */
private fun CapacityWorkload.health(): ServiceHealth? = when {
    riskLevel == CapacityRisk.OOM -> ServiceHealth.CRITICAL
    atRisk || noPriorityClass -> ServiceHealth.WARNING
    else -> null
}

/** Deployment, StatefulSet and DaemonSet as kubectl shortens them; any other kind as it is. */
@Composable
private fun kindShort(kind: String): String = when (kind) {
    "Deployment" -> stringResource(R.string.capacity_kind_deployment_short)
    "StatefulSet" -> stringResource(R.string.capacity_kind_statefulset_short)
    "DaemonSet" -> stringResource(R.string.capacity_kind_daemonset_short)
    else -> kind
}
