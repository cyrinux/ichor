package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ManagedNodeGroupRow
import name.levis.ichor.model.NODE_GROUP_MAX_SIZE
import name.levis.ichor.model.NODE_GROUP_UPDATE_UNREADABLE
import name.levis.ichor.model.NodeGroupSizeProblem
import name.levis.ichor.model.NodeGroupUpdate
import name.levis.ichor.model.canChangeSize
import name.levis.ichor.model.nodeGroupSizeProblem
import name.levis.ichor.model.podEvictionFailure
import name.levis.ichor.model.versionUpdate
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A node group's size change and upgrade: the update the last one started ([update], null
 * before one), whether one is being sent, how to send one, and [onFollow], which reads the
 * update while it runs (the sheet runs it while open). [clusterVersion] is the control plane's.
 */
data class NodeGroupScaling(
    val update: NodeGroupUpdate?,
    val busy: Boolean,
    val onScale: (desired: Int, min: Int, max: Int) -> Unit,
    val onFollow: suspend () -> Unit,
    val clusterVersion: String = "",
    val onUpgrade: (version: String, force: Boolean) -> Unit = { _, _ -> },
)

/**
 * "Change size…", then the typed name. AWS' refusal is the only gate: no Kubernetes verb is
 * involved. The update it starts is shown by [NodeGroupUpdateStatus].
 */
@Composable
fun NodeGroupScaleSection(row: ManagedNodeGroupRow, scaling: NodeGroupScaling) {
    var editing by remember { mutableStateOf(false) }
    var confirming by remember { mutableStateOf<Triple<Int, Int, Int>?>(null) }

    SectionTitle(stringResource(R.string.autoscaling_nodegroup_size_section))
    IchorOutlinedButton(onClick = { editing = true }, enabled = row.canChangeSize && !scaling.busy && scaling.update?.inProgress != true) {
        Text(stringResource(R.string.autoscaling_nodegroup_change_size))
    }
    if (!row.canChangeSize) MutedText(stringResource(R.string.autoscaling_nodegroup_size_updating))

    if (editing) {
        NodeGroupScaleDialog(
            row,
            onNext = { desired, min, max ->
                editing = false
                confirming = Triple(desired, min, max)
            },
            onDismiss = { editing = false },
        )
    }
    confirming?.let { (desired, min, max) ->
        HostnameConfirmDialog(
            title = stringResource(R.string.autoscaling_nodegroup_scale_confirm_title, row.name),
            hostname = row.name,
            confirmLabel = stringResource(R.string.autoscaling_nodegroup_change_size_confirm),
            onConfirm = {
                confirming = null
                scaling.onScale(desired, min, max)
            },
            onDismiss = { confirming = null },
        ) {
            Text(
                stringResource(R.string.autoscaling_nodegroup_scale_summary, desired.toString(), min.toString(), max.toString()),
                style = MaterialTheme.typography.bodyMedium,
            )
            if (desired < row.desired) {
                Text(stringResource(R.string.autoscaling_nodegroup_scale_down), style = MaterialTheme.typography.bodyMedium, color = LocalStatusColors.current.bad)
            }
        }
    }
}

/** Desired, minimum and maximum, prefilled from the group; Next stays off for a size AWS would refuse. */
@Composable
private fun NodeGroupScaleDialog(row: ManagedNodeGroupRow, onNext: (Int, Int, Int) -> Unit, onDismiss: () -> Unit) {
    var desired by rememberSaveable(row.key) { mutableIntStateOf(row.desired.coerceIn(0, NODE_GROUP_MAX_SIZE)) }
    var min by rememberSaveable(row.key) { mutableIntStateOf(row.min.coerceIn(0, NODE_GROUP_MAX_SIZE)) }
    var max by rememberSaveable(row.key) { mutableIntStateOf(row.max.coerceIn(1, NODE_GROUP_MAX_SIZE)) }
    val problem = nodeGroupSizeProblem(desired, min, max)
    val changed = desired != row.desired || min != row.min || max != row.max

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.autoscaling_nodegroup_scale_title, row.name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                ReplicaStepper(stringResource(R.string.autoscaling_nodegroup_desired), desired, 0..NODE_GROUP_MAX_SIZE) { desired = it }
                ReplicaStepper(stringResource(R.string.autoscaling_nodegroup_minimum), min, 0..NODE_GROUP_MAX_SIZE) { min = it }
                ReplicaStepper(stringResource(R.string.autoscaling_nodegroup_maximum), max, 1..NODE_GROUP_MAX_SIZE) { max = it }
                problem?.let {
                    Text(stringResource(it.message()), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
                MutedText(stringResource(R.string.autoscaling_nodegroup_scale_down))
                MutedText(stringResource(R.string.autoscaling_nodegroup_autoscaler_note))
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { onNext(desired, min, max) }, enabled = problem == null && changed) {
                Text(stringResource(R.string.autoscaling_nodegroup_scale_next))
            }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/**
 * The update a size change or an upgrade started: in progress (read every 10 s while the sheet
 * is open), done, or why it failed; a pod AWS could not evict is named.
 */
@Composable
fun NodeGroupUpdateStatus(scaling: NodeGroupScaling) {
    val u = scaling.update ?: return
    // Keyed on the update and its status: a new one restarts the read, its end stops it.
    LaunchedEffect(u.id, u.inProgress) { if (u.inProgress) scaling.onFollow() }
    val eviction = u.podEvictionFailure
    when {
        u.inProgress -> MutedText(
            stringResource(if (u.versionUpdate) R.string.autoscaling_nodegroup_upgrade_running else R.string.autoscaling_nodegroup_update_running),
        )
        u.successful -> MutedText(
            stringResource(if (u.versionUpdate) R.string.autoscaling_nodegroup_upgrade_done else R.string.autoscaling_nodegroup_update_done),
        )
        else -> Text(
            when {
                eviction != null -> stringResource(
                    R.string.autoscaling_nodegroup_upgrade_eviction_failed,
                    (eviction.resourceIds + eviction.message).filter { it.isNotEmpty() }.joinToString(": "),
                )
                else -> stringResource(
                    if (u.status == NODE_GROUP_UPDATE_UNREADABLE) R.string.autoscaling_nodegroup_update_unreadable
                    else R.string.autoscaling_nodegroup_update_failed,
                    u.failure.orEmpty(),
                )
            },
            style = MaterialTheme.typography.bodyMedium,
            color = LocalStatusColors.current.bad,
        )
    }
}

private fun NodeGroupSizeProblem.message(): Int = when (this) {
    NodeGroupSizeProblem.MIN_NEGATIVE -> R.string.autoscaling_nodegroup_size_min_negative
    NodeGroupSizeProblem.MAX_TOO_LOW -> R.string.autoscaling_nodegroup_size_max_low
    NodeGroupSizeProblem.MIN_ABOVE_MAX -> R.string.autoscaling_nodegroup_size_min_above_max
    NodeGroupSizeProblem.DESIRED_OUTSIDE -> R.string.autoscaling_nodegroup_size_desired_outside
}
