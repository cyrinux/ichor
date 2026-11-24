package name.levis.ichor.ui.autoscaling

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
import name.levis.ichor.model.AUTOSCALE_NOT_READY
import name.levis.ichor.model.AUTOSCALE_OK
import name.levis.ichor.model.ManagedNodeGroupRow
import name.levis.ichor.model.NodeGroupIssue
import name.levis.ichor.model.NodeGroupsReport
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/** The group header's error line: the action the role (AWS) or account (Google) may not call, or the provider's own words. */
@Composable
fun nodeGroupsError(report: NodeGroupsReport): String = when {
    report.forbidden.isEmpty() -> report.error
    report.gke -> stringResource(R.string.autoscaling_nodegroup_forbidden_account, report.forbidden)
    else -> stringResource(R.string.autoscaling_nodegroup_forbidden, report.forbidden)
}

/** The section title: "Node pools (EKS)" or "Node pools (GKE)". */
@Composable
fun nodeGroupsTitle(gke: Boolean): String =
    stringResource(if (gke) R.string.autoscaling_section_nodegroups_gke else R.string.autoscaling_section_nodegroups)

/** "3 of 1–6", or "Autoscaling off" for a GKE pool without bounds. */
@Composable
private fun nodeGroupSize(row: ManagedNodeGroupRow): String =
    if (row.autoscalingOff) {
        stringResource(R.string.autoscaling_nodegroup_autoscaling_off)
    } else {
        stringResource(R.string.autoscaling_nodegroup_size, row.desired.toString(), row.min.toString(), row.max.toString())
    }

/** "at max" in the warning colour, else ok / not ready from EKS' status. */
@Composable
private fun NodeGroupBadge(row: ManagedNodeGroupRow) {
    if (row.atMax) {
        Tag(stringResource(R.string.autoscaling_at_max), LocalStatusColors.current.bad, Modifier)
    } else {
        AutoscaleStateBadge(if (row.needsAttention) AUTOSCALE_NOT_READY else AUTOSCALE_OK)
    }
}

/**
 * Name, desired between its bounds, Kubernetes version, capacity type and the nodes it runs;
 * a group a minor behind its control plane says so.
 */
@Composable
fun NodeGroupRowItem(row: ManagedNodeGroupRow, onClick: () -> Unit) {
    ResourceRow(
        health = null,
        title = row.name,
        modifier = Modifier.padding(horizontal = 16.dp),
        subtitle = listOf(
            nodeGroupSize(row),
            row.version,
            row.capacityType,
            stringResource(R.string.autoscaling_pool_nodes, row.nodes.toString()),
        ).filter { it.isNotEmpty() }.joinToString("  ·  "),
        trailing = { NodeGroupBadge(row) },
        onClick = onClick,
    ) {
        if (row.behind) Tag(stringResource(R.string.autoscaling_nodegroup_behind), LocalStatusColors.current.warn, Modifier)
        row.issues.firstOrNull()?.let {
            Text(it.message, style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}

/**
 * Every field of one managed node group, and the provider's health issues with the resources they
 * name. A GKE pool ([gke]) says "Image type" and comes without [scaling].
 */
@Composable
fun NodeGroupSheet(row: ManagedNodeGroupRow, onDismiss: () -> Unit, scaling: NodeGroupScaling? = null, gke: Boolean = false) {
    Sheet(onDismiss) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
            NodeGroupBadge(row)
        }
        InfoRow(stringResource(R.string.autoscaling_nodegroup_status), row.status)
        InfoRow(stringResource(R.string.autoscaling_nodegroup_desired), row.desired.toString())
        if (row.autoscalingOff) {
            MutedText(stringResource(R.string.autoscaling_nodegroup_autoscaling_off))
        } else {
            InfoRow(stringResource(R.string.autoscaling_nodegroup_minimum), row.min.toString())
            InfoRow(stringResource(R.string.autoscaling_nodegroup_maximum), row.max.toString())
        }
        InfoRow(stringResource(R.string.autoscaling_nodegroup_nodes), row.nodes.toString())
        InfoRow(stringResource(R.string.autoscaling_nodegroup_version), row.version)
        if (row.releaseVersion.isNotEmpty()) InfoRow(stringResource(R.string.autoscaling_nodegroup_ami_release), row.releaseVersion)
        if (row.amiType.isNotEmpty()) {
            InfoRow(stringResource(if (gke) R.string.autoscaling_nodegroup_image_type else R.string.autoscaling_nodegroup_ami_type), row.amiType)
        }
        if (row.capacityType.isNotEmpty()) InfoRow(stringResource(R.string.autoscaling_nodegroup_capacity_type), row.capacityType)
        if (row.instanceTypes.isNotEmpty()) InfoRow(stringResource(R.string.autoscaling_nodegroup_instance_types), row.instanceTypes.joinToString(", "), mono = true)
        if (row.diskSize > 0) InfoRow(stringResource(R.string.autoscaling_nodegroup_disk), stringResource(R.string.autoscaling_nodegroup_disk_size, row.diskSize.toString()))
        if (row.created > 0) MutedText(timeAgo(row.created))
        scaling?.let {
            NodeGroupScaleSection(row, it)
            NodeGroupUpgradeSection(row, it)
            NodeGroupUpdateStatus(it)
        }

        SectionTitle(stringResource(R.string.autoscaling_nodegroup_issues))
        if (row.issues.isEmpty()) MutedText(stringResource(R.string.autoscaling_none))
        row.issues.forEach { IssueRow(it) }
    }
}

@Composable
private fun IssueRow(issue: NodeGroupIssue) {
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(issue.code, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, color = LocalStatusColors.current.bad)
        if (issue.message.isNotEmpty()) Text(issue.message, style = MaterialTheme.typography.bodyMedium)
        if (issue.resourceIds.isNotEmpty()) {
            Text(
                issue.resourceIds.joinToString(", "),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
