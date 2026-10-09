package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.FilterChip
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
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.TalosApp
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

/** A pod's CPU and memory in use against its limit, else its request (no bar when neither). */
@Composable
fun PodUsage(top: KubeTopPod, modifier: Modifier = Modifier) {
    val cpuOf = top.cpuLimit.takeIf { it > 0 } ?: top.cpuRequest
    val memoryOf = top.memoryLimit.takeIf { it > 0 } ?: top.memoryRequest
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        UsageLine(
            stringResource(R.string.node_live_cpu),
            formatCpu(top.cpu) + if (cpuOf > 0) " / ${formatCpu(cpuOf)}" else "",
            top.cpuFraction(),
        )
        UsageLine(
            stringResource(R.string.node_live_memory),
            formatBytes(top.memory.toLong()) + if (memoryOf > 0) " / ${formatBytes(memoryOf.toLong())}" else "",
            top.memoryFraction(),
        )
    }
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
            FilterChip(selected = sort == option, onClick = { onSort(option) }, label = { Text(stringResource(label)) })
        }
    }
}
