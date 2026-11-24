package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ManagedNodeGroupRow
import name.levis.ichor.model.NodeGroupUpgradeChoice
import name.levis.ichor.model.canChangeSize
import name.levis.ichor.model.customAmi
import name.levis.ichor.model.upgradeChoices
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorSwitch
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * "Upgrade…": the control plane's version (when the group is behind it) or the latest node
 * image of its own, an optional force, then the typed name. Every node is replaced: the
 * confirmation says so. The update it starts is shown by [NodeGroupUpdateStatus].
 */
@Composable
fun NodeGroupUpgradeSection(row: ManagedNodeGroupRow, scaling: NodeGroupScaling) {
    var picking by remember { mutableStateOf(false) }
    var confirming by remember { mutableStateOf<Pair<NodeGroupUpgradeChoice, Boolean>?>(null) }
    val choices = row.upgradeChoices(scaling.clusterVersion)

    SectionTitle(stringResource(R.string.autoscaling_nodegroup_upgrade_section))
    IchorOutlinedButton(onClick = { picking = true }, enabled = choices.isNotEmpty() && !scaling.busy && scaling.update?.inProgress != true) {
        Text(stringResource(R.string.autoscaling_nodegroup_upgrade))
    }
    when {
        !row.canChangeSize -> MutedText(stringResource(R.string.autoscaling_nodegroup_upgrade_unavailable))
        row.customAmi -> MutedText(stringResource(R.string.autoscaling_nodegroup_upgrade_custom_ami))
    }

    if (picking) {
        NodeGroupUpgradeDialog(
            row,
            choices,
            onNext = { choice, force ->
                picking = false
                confirming = choice to force
            },
            onDismiss = { picking = false },
        )
    }
    confirming?.let { (choice, force) ->
        HostnameConfirmDialog(
            title = stringResource(R.string.autoscaling_nodegroup_upgrade_confirm_title, row.name),
            hostname = row.name,
            confirmLabel = stringResource(R.string.autoscaling_nodegroup_upgrade_confirm),
            onConfirm = {
                confirming = null
                scaling.onUpgrade(choice.version, force)
            },
            onDismiss = { confirming = null },
        ) {
            Text(choiceLabel(choice, row), style = MaterialTheme.typography.bodyMedium)
            Text(stringResource(R.string.autoscaling_nodegroup_upgrade_replace), style = MaterialTheme.typography.bodyMedium)
            if (force) {
                Text(
                    stringResource(R.string.autoscaling_nodegroup_upgrade_force_on),
                    style = MaterialTheme.typography.bodyMedium,
                    color = LocalStatusColors.current.bad,
                )
            }
        }
    }
}

@Composable
private fun choiceLabel(choice: NodeGroupUpgradeChoice, row: ManagedNodeGroupRow): String = when (choice) {
    is NodeGroupUpgradeChoice.Kubernetes -> stringResource(R.string.autoscaling_nodegroup_upgrade_kubernetes, choice.version)
    NodeGroupUpgradeChoice.LatestImage -> stringResource(R.string.autoscaling_nodegroup_upgrade_latest_image, row.version)
}

/** The upgrade to run (the control plane's version first) and the force switch, off by default. */
@Composable
private fun NodeGroupUpgradeDialog(
    row: ManagedNodeGroupRow,
    choices: List<NodeGroupUpgradeChoice>,
    onNext: (NodeGroupUpgradeChoice, Boolean) -> Unit,
    onDismiss: () -> Unit,
) {
    var chosen by rememberSaveable(row.key) { mutableIntStateOf(0) }
    var force by rememberSaveable(row.key) { mutableStateOf(false) }
    val choice = choices.getOrNull(chosen) ?: choices.firstOrNull()

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.autoscaling_nodegroup_upgrade_title, row.name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                choices.forEachIndexed { i, c ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.fillMaxWidth().selectable(selected = c == choice, role = Role.RadioButton, onClick = { chosen = i }),
                    ) {
                        RadioButton(selected = c == choice, onClick = null)
                        Text(choiceLabel(c, row))
                    }
                }
                MutedText(stringResource(R.string.autoscaling_nodegroup_upgrade_replace))
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth().toggleable(value = force, role = Role.Switch, onValueChange = { force = it }),
                ) {
                    Text(stringResource(R.string.autoscaling_nodegroup_upgrade_force), modifier = Modifier.weight(1f))
                    IchorSwitch(checked = force, onCheckedChange = null, modifier = Modifier.padding(start = 12.dp))
                }
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { choice?.let { onNext(it, force) } }, enabled = choice != null) {
                Text(stringResource(R.string.autoscaling_nodegroup_scale_next))
            }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
