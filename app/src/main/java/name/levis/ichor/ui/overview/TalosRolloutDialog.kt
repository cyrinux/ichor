package name.levis.ichor.ui.overview

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.EtcdHealth
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.Rollout
import name.levis.ichor.model.RolloutHold
import name.levis.ichor.model.RolloutRow
import name.levis.ichor.model.RolloutRun
import name.levis.ichor.model.RolloutState
import name.levis.ichor.model.health
import name.levis.ichor.model.rollout
import name.levis.ichor.model.upgradeWaitsForNode
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The rolling upgrade to [latest] as a plan to come back to: every node under its role in
 * upgrade order with where it stands, what holds the rollout up, and one button for the next
 * node. The rows are the expert path; a worker ahead of the control plane asks first.
 */
@Composable
fun TalosRolloutDialog(
    nodes: List<NodeOverview>,
    latest: String,
    onUpgrade: (NodeOverview) -> Unit,
    onDismiss: () -> Unit,
    /** Every node in one roll, the app driving it (the cluster upgrade); null: not offered. */
    onUpgradeAll: (() -> Unit)? = null,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val run by app.upgradeManager.current.collectAsStateWithLifecycle()
    // Read again when a node's health or version changes. Unknown when it cannot be read: the
    // upgrade screen's own etcd checks still apply.
    val healthKey = remember(nodes) { nodes.map { Triple(it.reachable, it.ready, it.version) } }
    val etcd by produceState<EtcdHealth?>(null, healthKey) {
        // Asked again while a member is unhealthy: it recovers without any node changing in the overview.
        while (true) {
            value = try {
                app.talosRepository.etcd().health()
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                null
            }
            if (value?.degraded != true) break
            delay(ETCD_RECHECK_MILLIS)
        }
    }
    val rollout = remember(nodes, latest, run, etcd) {
        val followed = run?.let { RolloutRun(it.node, upgradeWaitsForNode(it.events), it.finished, it.error != null, it.newVersion) }
        rollout(nodes, latest, followed, etcd)
    }
    var anyway by remember { mutableStateOf<RolloutRow?>(null) }

    fun open(row: RolloutRow) {
        // A failed run is forgotten, so that the upgrade screen plans a new one instead of showing it.
        if (row.state == RolloutState.FAILED) app.upgradeManager.dismiss()
        onUpgrade(row.node)
    }

    fun pick(row: RolloutRow) {
        if (rollout.needsConfirm(row)) anyway = row else open(row)
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.overview_talos_pick_node, latest)) },
        text = {
            Column {
                rollout.hold?.let { RolloutHoldLine(it, rollout.etcd) }
                // Scrolls under the pinned status: a large cluster lists more nodes than fit the dialog.
                Column(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState())) {
                    RolloutSection(
                        stringResource(R.string.overview_talos_rollout_control_plane, rollout.controlPlaneDone, rollout.controlPlane.size),
                        rollout.controlPlane,
                        rollout,
                        onPick = { pick(it) },
                    )
                    RolloutSection(
                        stringResource(R.string.overview_talos_rollout_workers, rollout.workersDone, rollout.workers.size),
                        rollout.workers,
                        rollout,
                        onPick = { pick(it) },
                        // A dimmed row with no reason looks broken: say why, right above them.
                        note = if (rollout.workersWait) stringResource(R.string.overview_talos_rollout_workers_wait) else null,
                    )
                }
            }
        },
        confirmButton = {
            rollout.next?.let { next ->
                Button(onClick = { pick(next) }, enabled = rollout.canOpen(next)) {
                    Text(stringResource(R.string.overview_talos_rollout_next, next.node.hostname))
                }
            }
        },
        dismissButton = {
            Row {
                onUpgradeAll?.let { TextButton(onClick = it) { Text(stringResource(R.string.cluster_upgrade_all)) } }
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) }
            }
        },
    )

    anyway?.let { row ->
        ConfirmDialog(
            title = stringResource(R.string.overview_talos_rollout_anyway_title, row.node.hostname),
            text = stringResource(R.string.overview_talos_rollout_anyway_body, rollout.controlPlaneDone, rollout.controlPlane.size),
            confirm = stringResource(R.string.overview_talos_rollout_anyway),
            onConfirm = {
                anyway = null
                open(row)
            },
            onDismiss = { anyway = null },
        )
    }
}

private const val ETCD_RECHECK_MILLIS = 5_000L

/** Why nothing else can start now, pinned above the list. */
@Composable
private fun RolloutHoldLine(hold: RolloutHold, etcd: EtcdHealth?) {
    val text = when (hold) {
        is RolloutHold.Upgrading -> {
            val status = stringResource(
                if (hold.waiting) R.string.overview_talos_rollout_waiting else R.string.overview_talos_rollout_upgrading,
                hold.node.hostname,
            )
            // Only a control-plane node takes an etcd member down with it.
            if (etcd == null || hold.node.role != "controlplane") status
            else status + " · " + stringResource(R.string.overview_talos_rollout_etcd_short, etcd.healthy, etcd.members)
        }
        is RolloutHold.Unhealthy -> hold.nodes.first().let {
            stringResource(if (it.reachable) R.string.overview_talos_rollout_not_ready else R.string.overview_talos_rollout_unreachable, it.hostname)
        }
        is RolloutHold.Etcd -> stringResource(R.string.overview_talos_rollout_etcd, hold.health.healthy, hold.health.members)
    }
    val upgrading = hold is RolloutHold.Upgrading
    Row(
        Modifier.fillMaxWidth().padding(bottom = 8.dp).semantics { liveRegion = LiveRegionMode.Polite },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (upgrading) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
        Text(
            text,
            style = MaterialTheme.typography.bodyMedium,
            color = if (upgrading) MaterialTheme.colorScheme.onSurface else LocalStatusColors.current.bad,
        )
    }
}

@Composable
private fun RolloutSection(title: String, rows: List<RolloutRow>, rollout: Rollout, onPick: (RolloutRow) -> Unit, note: String? = null) {
    if (rows.isEmpty()) return
    SectionTitle(title)
    if (note != null) MutedText(note)
    rows.forEach { row -> RolloutNodeRow(row, enabled = rollout.canOpen(row), later = note != null) { onPick(row) } }
}

/** A row that cannot be opened now (Material's disabled content). */
private const val HELD_ROW_ALPHA = 0.38f

/** Done, or better left for [later]: less prominent, yet clearly not disabled. */
private const val LATER_ROW_ALPHA = 0.6f

@Composable
private fun RolloutNodeRow(row: RolloutRow, enabled: Boolean, later: Boolean, onClick: () -> Unit) {
    val active = row.state == RolloutState.UPGRADING || row.state == RolloutState.WAITING_HEALTHY
    val alpha = when {
        active -> 1f
        !enabled && !row.done -> HELD_ROW_ALPHA
        row.done || later -> LATER_ROW_ALPHA
        else -> 1f
    }
    Row(
        Modifier.fillMaxWidth().clickable(enabled = enabled, onClick = onClick).padding(vertical = 8.dp).alpha(alpha),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(row.node.hostname, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
            Text(
                listOf(row.node.version, row.node.node).filter { it.isNotBlank() }.joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        RolloutStateMark(row.state, enabled)
    }
}

/** Where the node stands, at the end of its row; a chevron marks a pending one that can be opened. */
@Composable
private fun RolloutStateMark(state: RolloutState, enabled: Boolean) {
    val colors = LocalStatusColors.current
    when (state) {
        RolloutState.PENDING -> if (enabled) {
            Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        RolloutState.UPGRADING, RolloutState.WAITING_HEALTHY -> {
            CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            Text(
                stringResource(
                    if (state == RolloutState.UPGRADING) R.string.overview_talos_rollout_state_upgrading
                    else R.string.overview_talos_rollout_state_waiting,
                ),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
        RolloutState.DONE -> Icon(Icons.Outlined.CheckCircle, contentDescription = stringResource(R.string.upgrade_step_done), tint = colors.ok)
        RolloutState.FAILED -> Text(
            stringResource(if (enabled) R.string.overview_talos_rollout_state_retry else R.string.upgrade_step_failed),
            style = MaterialTheme.typography.labelMedium,
            color = colors.bad,
        )
    }
}
