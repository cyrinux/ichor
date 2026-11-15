package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeTopContainer
import name.levis.ichor.model.KubeTopNode
import name.levis.ichor.model.KubeTopNodes
import name.levis.ichor.model.KubeTopPod
import name.levis.ichor.model.KubeTopPods
import name.levis.ichor.model.TopSort
import name.levis.ichor.model.formatCpu
import name.levis.ichor.util.formatBytes

/**
 * Node usage from metrics-server, read again whenever [key] changes (pass the node list, so a
 * refresh refreshes it too). Null while loading, on error, or without metrics-server: the rows
 * then simply show no bars.
 */
@Composable
fun rememberKubeTopNodes(key: Any?): KubeTopNodes? {
    if (LocalInspectionMode.current) return null
    val app = LocalContext.current.applicationContext as? TalosApp ?: return null
    val top by produceState<KubeTopNodes?>(null, key) {
        value = quietly { app.kubeRepository.topNodes() }?.takeIf { it.available }
    }
    return top
}

/** Pod usage of [namespace] (null: all), like [rememberKubeTopNodes]. */
@Composable
fun rememberKubeTopPods(namespace: String?, key: Any?): KubeTopPods? {
    if (LocalInspectionMode.current) return null
    val app = LocalContext.current.applicationContext as? TalosApp ?: return null
    val top by produceState<KubeTopPods?>(null, namespace, key) {
        value = quietly { app.kubeRepository.topPods(namespace) }?.takeIf { it.available }
    }
    return top
}

/** One pod's usage and bounds, read again when [key] changes; null as in [rememberKubeTopNodes]. */
@Composable
fun rememberKubeTopPod(namespace: String, name: String, key: Any? = null): KubeTopPod? {
    if (LocalInspectionMode.current) return null
    val app = LocalContext.current.applicationContext as? TalosApp ?: return null
    val top by produceState<KubeTopPod?>(null, namespace, name, key) {
        value = quietly { app.kubeRepository.topPod(namespace, name) }?.takeIf { it.available }?.pods?.firstOrNull()
    }
    return top
}

private suspend fun <T> quietly(block: suspend () -> T): T? = try {
    block()
} catch (e: CancellationException) {
    throw e
} catch (_: Exception) {
    null
}

/** A node's CPU and memory in use against what it can give. */
@Composable
fun NodeUsage(top: KubeTopNode, modifier: Modifier = Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        UsageLine(
            stringResource(R.string.node_live_cpu),
            "${formatCpu(top.cpu)} / ${formatCpu(top.cpuAllocatable)}",
            (top.cpuPercent / 100).toFloat(),
        )
        UsageLine(
            stringResource(R.string.node_live_memory),
            "${formatBytes(top.memory.toLong())} / ${formatBytes(top.memoryAllocatable.toLong())}",
            (top.memoryPercent / 100).toFloat(),
        )
    }
}

/**
 * A pod's CPU and memory in use against its limit, else its request (no bar when neither). With
 * two or more containers, each one's usage against its own bounds follows, in the spec's order,
 * as `kubectl top pod NAME --containers`; a single container would only repeat the pod.
 */
@Composable
fun PodUsage(top: KubeTopPod, modifier: Modifier = Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        UsageLines(
            top.cpu, top.cpuLimit, top.cpuRequest, top.cpuFraction(),
            top.memory, top.memoryLimit, top.memoryRequest, top.memoryFraction(),
        )
        if (top.containers.size > 1) {
            Text(
                stringResource(R.string.top_containers),
                style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.padding(top = 6.dp),
            )
            top.containers.forEach { ContainerUsage(it) }
        }
    }
}

/** One container: its name, then its CPU and memory lines, indented under the pod's. */
@Composable
private fun ContainerUsage(c: KubeTopContainer) {
    Column(Modifier.padding(start = 12.dp, top = 2.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(
            c.name,
            style = MaterialTheme.typography.labelSmall,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        UsageLines(
            c.cpu, c.cpuLimit, c.cpuRequest, c.cpuFraction(),
            c.memory, c.memoryLimit, c.memoryRequest, c.memoryFraction(),
        )
    }
}

/** The CPU and Memory lines of a pod or a container: used, then " / " its limit, else its request. */
@Composable
private fun UsageLines(
    cpu: Double, cpuLimit: Double, cpuRequest: Double, cpuFraction: Float?,
    memory: Double, memoryLimit: Double, memoryRequest: Double, memoryFraction: Float?,
) {
    val cpuOf = cpuLimit.takeIf { it > 0 } ?: cpuRequest
    val memoryOf = memoryLimit.takeIf { it > 0 } ?: memoryRequest
    UsageLine(
        stringResource(R.string.node_live_cpu),
        formatCpu(cpu) + if (cpuOf > 0) " / ${formatCpu(cpuOf)}" else "",
        cpuFraction,
    )
    UsageLine(
        stringResource(R.string.node_live_memory),
        formatBytes(memory.toLong()) + if (memoryOf > 0) " / ${formatBytes(memoryOf.toLong())}" else "",
        memoryFraction,
    )
}

/** "CPU  [bar]  125m / 1": read as one phrase by screen readers. */
@Composable
private fun UsageLine(label: String, value: String, fraction: Float?) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(
        Modifier.fillMaxWidth().clearAndSetSemantics { contentDescription = "$label $value" },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.width(52.dp))
        if (fraction != null) UsageBar(fraction, Modifier.weight(1f)) else Spacer(Modifier.weight(1f))
        Text(value, style = MaterialTheme.typography.labelSmall, color = muted)
    }
}

/** "Sort by: Name · CPU · Memory", for a list that has usage. */
@Composable
fun TopSortChips(sort: TopSort, onSort: (TopSort) -> Unit, modifier: Modifier = Modifier) {
    Row(modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.node_processes_sort), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        listOf(
            TopSort.NAME to R.string.images_sort_name,
            TopSort.CPU to R.string.node_processes_sort_cpu,
            TopSort.MEMORY to R.string.node_processes_sort_memory,
        ).forEach { (option, label) ->
            IchorFilterChip(selected = sort == option, onClick = { onSort(option) }, label = { Text(stringResource(label)) })
        }
    }
}
