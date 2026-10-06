package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DragonflyInstance
import name.levis.ichor.model.DragonflyPod
import name.levis.ichor.model.DragonflyReason
import name.levis.ichor.model.DragonflyStatus
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError

/** Every Dragonfly instance (problems first) with its pods and their master/replica role. */
@Composable
fun DragonflyTab(status: DragonflyStatus) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.instances.isEmpty()) item { EmptyText(stringResource(R.string.dragonfly_empty)) }
        items(status.instances, key = { it.label }) { inst ->
            InstanceRow(inst)
            HorizontalDivider()
        }
    }
}

@Composable
private fun InstanceRow(inst: DragonflyInstance) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        val line = listOf(stringResource(R.string.pods_ready_count, inst.readyPods, inst.replicas)) + inst.reasonList.map { reasonText(it, inst) }
        ServiceRowHeader(inst.serviceHealth, inst.label, line)
        inst.pods.forEach { DataPodLine(it.name, it.ready, it.node, it.phase, it.role) }
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
