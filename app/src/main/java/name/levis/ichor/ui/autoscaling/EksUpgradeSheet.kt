package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.EksUpgradePlan
import name.levis.ichor.model.EksUpgradeState
import name.levis.ichor.model.NodeGroupUpdate
import name.levis.ichor.model.state
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatDate

/**
 * The control plane's upgrade: the plan, the update this screen follows ([update], null before
 * one), whether one is being sent, how to start one and [onFollow], which reads the update while
 * it runs (the sheet runs it while open, from the plan's update when the app was reopened).
 */
data class EksUpgradeControl(
    val plan: EksUpgradePlan,
    val update: NodeGroupUpdate?,
    val busy: Boolean,
    val onUpgrade: (version: String) -> Unit,
    val onFollow: suspend () -> Unit,
)

/** The control plane row's line: the version and what comes next, or the update running. */
@Composable
private fun eksUpgradeLine(plan: EksUpgradePlan, update: NodeGroupUpdate?): String = when (plan.state(update)) {
    EksUpgradeState.RUNNING -> stringResource(R.string.autoscaling_eks_upgrade_row_running, plan.version, (update ?: plan.updating)?.status.orEmpty())
    EksUpgradeState.AVAILABLE -> stringResource(R.string.autoscaling_eks_upgrade_row_available, plan.version, plan.next)
    EksUpgradeState.UNCHECKED -> stringResource(R.string.autoscaling_eks_upgrade_row_unchecked, plan.version, plan.next)
    EksUpgradeState.UP_TO_DATE -> stringResource(R.string.autoscaling_eks_upgrade_row_up_to_date, plan.version)
    EksUpgradeState.UNKNOWN -> plan.error
}

/** "Control plane" at the top of the EKS group: Kubernetes version, the next minor or the update running. */
@Composable
fun EksControlPlaneRow(plan: EksUpgradePlan, update: NodeGroupUpdate?, onClick: () -> Unit) {
    val colors = LocalStatusColors.current
    ResourceRow(
        health = null,
        title = stringResource(R.string.autoscaling_eks_control_plane),
        modifier = Modifier.padding(horizontal = 16.dp),
        subtitle = eksUpgradeLine(plan, update),
        mono = false,
        trailing = {
            when (plan.state(update)) {
                EksUpgradeState.RUNNING -> Tag(stringResource(R.string.autoscaling_eks_upgrade_tag_running), colors.warn, Modifier)
                EksUpgradeState.AVAILABLE, EksUpgradeState.UNCHECKED ->
                    Tag(stringResource(R.string.autoscaling_eks_upgrade_tag_available), colors.warn, Modifier)
                else -> {}
            }
        },
        onClick = onClick,
    )
}

/**
 * The plan before anything starts: current and next version, end of standard support, the
 * blockers, what the upgrade cannot undo, the node groups to upgrade afterwards; then
 * "Upgrade…" with the cluster's name typed. The update is followed every 30 s while open.
 */
@Composable
fun EksUpgradeSheet(control: EksUpgradeControl, onDismiss: () -> Unit) {
    val plan = control.plan
    val update = control.update ?: plan.updating
    var confirming by remember { mutableStateOf(false) }
    val bad = LocalStatusColors.current.bad
    // Keyed on the update and its status: a new one restarts the read, its end stops it.
    LaunchedEffect(update?.id, update?.inProgress) { if (update?.inProgress == true) control.onFollow() }

    Sheet(onDismiss) {
        Text(stringResource(R.string.autoscaling_eks_control_plane), style = MaterialTheme.typography.titleMedium)
        Text(plan.cluster, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        if (plan.error.isNotEmpty()) InlineError(eksPlanError(plan))
        InfoRow(stringResource(R.string.autoscaling_eks_upgrade_current), plan.version)
        InfoRow(stringResource(R.string.autoscaling_nodegroup_status), plan.status)
        if (plan.platformVersion.isNotEmpty()) InfoRow(stringResource(R.string.autoscaling_eks_upgrade_platform), plan.platformVersion)
        InfoRow(stringResource(R.string.autoscaling_eks_upgrade_next), nextText(plan))
        if (plan.supportEnds > 0) InfoRow(stringResource(R.string.autoscaling_eks_upgrade_support_ends), formatDate(plan.supportEnds))

        SectionTitle(stringResource(R.string.autoscaling_eks_upgrade_blockers))
        if (plan.blockers.isEmpty()) MutedText(stringResource(R.string.autoscaling_none))
        plan.blockers.forEach { Text(it, style = MaterialTheme.typography.bodyMedium, color = bad) }

        if (plan.next.isNotEmpty()) {
            SectionTitle(stringResource(R.string.autoscaling_nodegroup_upgrade_section))
            Text(stringResource(R.string.autoscaling_eks_upgrade_irreversible), style = MaterialTheme.typography.bodyMedium)
            if (plan.groups.isNotEmpty()) {
                MutedText(stringResource(R.string.autoscaling_eks_upgrade_groups_after, plan.next, plan.groups.joinToString(", ")))
            }
            IchorOutlinedButton(onClick = { confirming = true }, enabled = plan.canUpgrade && !control.busy && update?.inProgress != true) {
                Text(stringResource(R.string.autoscaling_nodegroup_upgrade))
            }
        }
        update?.let { EksUpdateStatus(it) }
    }

    if (confirming) {
        HostnameConfirmDialog(
            title = stringResource(R.string.autoscaling_eks_upgrade_confirm_title, plan.cluster, plan.next),
            hostname = plan.cluster,
            confirmLabel = stringResource(R.string.autoscaling_nodegroup_upgrade_confirm),
            onConfirm = {
                confirming = false
                control.onUpgrade(plan.next)
            },
            onDismiss = { confirming = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.autoscaling_eks_upgrade_summary, plan.version, plan.next), style = MaterialTheme.typography.bodyMedium)
            Text(stringResource(R.string.autoscaling_eks_upgrade_irreversible), style = MaterialTheme.typography.bodyMedium, color = bad)
        }
    }
}

@Composable
private fun nextText(plan: EksUpgradePlan): String = when {
    plan.next.isEmpty() && plan.version.isNotEmpty() -> stringResource(R.string.autoscaling_eks_upgrade_up_to_date)
    plan.next.isNotEmpty() && !plan.nextKnown -> stringResource(R.string.autoscaling_eks_upgrade_unchecked, plan.next)
    else -> plan.next
}

/** The plan's error line: the AWS action the role may not call, or AWS' own words. */
@Composable
private fun eksPlanError(plan: EksUpgradePlan): String =
    if (plan.forbidden.isNotEmpty()) stringResource(R.string.autoscaling_nodegroup_forbidden, plan.forbidden) else plan.error

/** The upgrade AWS runs: in progress, done, or AWS' errors. */
@Composable
private fun EksUpdateStatus(u: NodeGroupUpdate) {
    when {
        u.inProgress -> MutedText(stringResource(R.string.autoscaling_eks_upgrade_running))
        u.successful -> MutedText(stringResource(R.string.autoscaling_eks_upgrade_done))
        else -> Text(
            stringResource(R.string.autoscaling_nodegroup_update_failed, u.failure.orEmpty()),
            style = MaterialTheme.typography.bodyMedium,
            color = LocalStatusColors.current.bad,
        )
    }
}
