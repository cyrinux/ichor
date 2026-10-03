package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DragonflyInstance
import name.levis.ichor.model.DragonflyPod
import name.levis.ichor.model.DragonflyReason
import name.levis.ichor.model.DragonflyStatus
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.theme.LocalStatusColors

/** Every Dragonfly instance (problems first) with its pods and their master/replica role. */
@Composable
fun DragonflyTab(status: DragonflyStatus) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.instances.isEmpty()) item { EmptyLine(stringResource(R.string.dragonfly_empty)) }
        items(status.instances, key = { it.label }) { inst ->
            InstanceRow(inst)
            HorizontalDivider()
        }
    }
}

@Composable
private fun InstanceRow(inst: DragonflyInstance) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(inst.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(inst.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        val line = listOf(stringResource(R.string.pods_ready_count, inst.readyPods, inst.replicas)) + inst.reasonList.map { reasonText(it, inst) }
        Text(
            line.joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = inst.serviceHealth.color().takeIf { inst.serviceHealth.needsAttention } ?: muted,
            modifier = Modifier.padding(start = 22.dp),
        )
        inst.pods.forEach { PodLine(it) }
    }
}

@Composable
private fun PodLine(p: DragonflyPod) {
    val role = when (p.role) {
        "master" -> stringResource(R.string.dragonfly_role_master)
        "replica" -> stringResource(R.string.cnpg_role_replica)
        else -> p.role.ifEmpty { null }
    }
    val where = when {
        p.node.isEmpty() && p.phase == "Pending" -> stringResource(R.string.cnpg_pod_pending)
        p.node.isNotEmpty() -> stringResource(R.string.data_services_on_node, p.node)
        else -> p.phase
    }
    Row(Modifier.padding(start = 22.dp, top = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(if (p.ready) ServiceHealth.OK else ServiceHealth.CRITICAL, Modifier.size(8.dp))
        Spacer(Modifier.size(8.dp))
        Text(
            listOfNotNull(p.name, role, where).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = if (p.ready) MaterialTheme.colorScheme.onSurface else LocalStatusColors.current.bad,
        )
    }
}

@Composable
private fun reasonText(reason: DragonflyReason, inst: DragonflyInstance): String = when (reason) {
    DragonflyReason.NO_READY -> stringResource(R.string.dragonfly_reason_no_ready)
    DragonflyReason.NO_MASTER -> stringResource(R.string.dragonfly_reason_no_master)
    DragonflyReason.MASTERS -> stringResource(R.string.dragonfly_reason_masters)
    DragonflyReason.PODS -> stringResource(R.string.dragonfly_reason_pods)
    // The operator's phase is its own word (a rolling update, replication being set up).
    DragonflyReason.NOT_READY -> stringResource(R.string.dragonfly_reason_phase, inst.phase)
}
