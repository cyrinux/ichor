package name.levis.ichor.ui.lifecycle

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
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
import name.levis.ichor.model.CapiCluster
import name.levis.ichor.model.CapiMachine
import name.levis.ichor.model.CapiMachineDeployment
import name.levis.ichor.model.CapiReplicas
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.SideroServer
import name.levis.ichor.model.SideroServerClass
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.timeAgo

/**
 * One row: health dot and name, the details that matter, then (for a problem) the failure or
 * the conditions that are not True, in full.
 */
@Composable
private fun RowShell(health: ServiceHealth, title: String, line: String, problem: String, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            HealthDot(health)
            Text(
                title,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
        }
        line.takeIf { it.isNotEmpty() }?.let { MutedText(it, maxLines = 3, overflow = TextOverflow.Ellipsis) }
        if (health.needsAttention && problem.isNotEmpty()) {
            Text(
                problem,
                style = MaterialTheme.typography.bodySmall,
                color = if (health == ServiceHealth.CRITICAL) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

private fun ratio(r: CapiReplicas) = "${r.ready}/${r.desired}"

/** A workload cluster: its phase, control plane and workers ready of desired, endpoint and versions. */
@Composable
fun ClusterRow(c: CapiCluster, onClick: () -> Unit) {
    val line = listOfNotNull(
        c.phase.takeIf { it.isNotEmpty() },
        c.controlPlane?.let { stringResource(R.string.cluster_lifecycle_control_plane_count, ratio(it)) },
        c.workers.takeIf { it.desired > 0 || it.ready > 0 }?.let { stringResource(R.string.cluster_lifecycle_workers_count, ratio(it)) },
        c.endpoint.takeIf { it.isNotEmpty() },
        c.versions.takeIf { it.isNotEmpty() }?.joinToString(", "),
        stringResource(R.string.cluster_lifecycle_paused).takeIf { c.paused },
    ).joinToString(" · ")
    RowShell(c.serviceHealth, c.name, line, c.problem, onClick)
}

/** A Machine: its phase, role, node, version and addresses, and how long it has been stuck. */
@Composable
fun MachineRow(m: CapiMachine, onClick: () -> Unit) {
    val line = listOfNotNull(
        m.phase.takeIf { it.isNotEmpty() },
        stringResource(R.string.cluster_lifecycle_control_plane).takeIf { m.controlPlane },
        m.node.takeIf { it.isNotEmpty() },
        m.version.takeIf { it.isNotEmpty() },
        m.addresses.takeIf { it.isNotEmpty() }?.joinToString(", "),
        if (!m.stuck) null
        else if (m.stuckSince > 0) stringResource(R.string.cluster_lifecycle_stuck, timeAgo(m.stuckSince))
        else stringResource(R.string.cluster_lifecycle_stuck_plain),
    ).joinToString(" · ")
    RowShell(m.serviceHealth, m.name, line, m.problem, onClick)
}

/** A MachineDeployment: its phase, its replicas ready and available of desired, and version. */
@Composable
fun MachineDeploymentRow(d: CapiMachineDeployment, onClick: () -> Unit) {
    val line = listOfNotNull(
        d.phase.takeIf { it.isNotEmpty() },
        stringResource(R.string.cluster_lifecycle_md_counts, d.ready.toString(), d.available.toString(), d.desired.toString()),
        d.version.takeIf { it.isNotEmpty() },
    ).joinToString(" · ")
    RowShell(d.serviceHealth, d.name, line, "", onClick)
}

/** A bare-metal server: accepted, cordoned, in use, clean; its power, classes, CPU and memory. */
@Composable
fun ServerRow(s: SideroServer, onClick: () -> Unit) {
    val line = listOfNotNull(
        s.name.takeIf { s.hostname.isNotEmpty() && it != s.hostname },
        stringResource(if (s.accepted) R.string.cluster_lifecycle_accepted else R.string.cluster_lifecycle_not_accepted),
        stringResource(R.string.cluster_lifecycle_cordoned).takeIf { s.cordoned },
        stringResource(R.string.cluster_lifecycle_in_use).takeIf { s.inUse },
        stringResource(R.string.cluster_lifecycle_clean).takeIf { s.isClean },
        s.power.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.cluster_lifecycle_power, it) },
        s.serverClasses.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.cluster_lifecycle_classes, it.joinToString(", ")) },
        s.addresses.takeIf { it.isNotEmpty() }?.joinToString(", "),
        s.cpu.takeIf { it.isNotEmpty() },
        s.memoryBytes.takeIf { it > 0 }?.let { formatBytes(it) },
    ).joinToString(" · ")
    RowShell(s.serviceHealth, s.hostname.ifEmpty { s.name }, line, "", onClick)
}

/** A ServerClass: how many of its servers are free and in use. */
@Composable
fun ServerClassRow(sc: SideroServerClass, onClick: () -> Unit) {
    val health = if (sc.available == 0 && sc.inUse > 0) ServiceHealth.WARNING else ServiceHealth.OK
    RowShell(health, sc.name, stringResource(R.string.cluster_lifecycle_class_counts, sc.available.toString(), sc.inUse.toString()), "", onClick)
}
