package name.levis.ichor.ui.overview

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Balance
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.DataUsage
import androidx.compose.material.icons.outlined.Dataset
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Gavel
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.TaskAlt
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Upgrade
import androidx.compose.material.icons.outlined.VpnLock
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.components.SectionCard

/** One screen the tools card opens. */
private data class KubeTool(val label: String, val icon: ImageVector, val onClick: () -> Unit)

/** A heading over the screens of one family. */
private data class KubeToolGroup(val title: String, val tools: List<KubeTool>)

private val TileSize = 36.dp
private val RowPadding = 4.dp
private val TileGap = 12.dp
private val ChevronSize = 24.dp
private val ColumnGap = 8.dp

/** Everything in a row that is not the label: padding, icon tile, gaps and chevron. */
private val RowChrome = RowPadding * 2 + TileSize + TileGap + RowPadding + ChevronSize

/**
 * The Kubernetes screens that work with these credentials alone, grouped by family: an icon
 * on a quiet secondary tile, the label in onSurface and a chevron. Primary stays for selection.
 */
@Composable
internal fun KubeToolsCard(
    nav: KubeHomeNavigation,
    crossplane: Boolean = false,
    tailscale: Boolean = false,
    clusterLifecycle: Boolean = false,
    argoRollouts: Boolean = false,
) {
    val groups = kubeToolGroups(nav, crossplane = crossplane, tailscale = tailscale, clusterLifecycle = clusterLifecycle, argoRollouts = argoRollouts)
    val labelStyle = MaterialTheme.typography.bodyLarge
    SectionCard(stringResource(R.string.common_kind_kubernetes)) {
        BoxWithConstraints(Modifier.fillMaxWidth()) {
            val twoColumns = labelsFitTwoColumns(groups.flatMap { g -> g.tools.map { it.label } }, labelStyle, maxWidth)
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                groups.forEachIndexed { index, group ->
                    if (index > 0) Spacer(Modifier.height(8.dp))
                    KubeToolGroupSection(group, twoColumns, labelStyle)
                }
            }
        }
    }
}

/**
 * Two columns only when the widest label, measured at the user's text size, fits on one line
 * in half the width; otherwise one column, where a long label wraps instead of being cut.
 */
@Composable
private fun labelsFitTwoColumns(labels: List<String>, style: TextStyle, width: Dp): Boolean {
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val widest = labels.maxOfOrNull { measurer.measure(it, style, softWrap = false, maxLines = 1).size.width } ?: 0
    val room = (width - ColumnGap) / 2 - RowChrome
    return with(density) { widest.toDp() } <= room
}

@Composable
private fun KubeToolGroupSection(group: KubeToolGroup, twoColumns: Boolean, labelStyle: TextStyle) {
    Text(
        group.title,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(horizontal = RowPadding).semantics { heading() },
    )
    if (!twoColumns) {
        group.tools.forEach { KubeToolRow(it, labelStyle, Modifier.fillMaxWidth()) }
        return
    }
    group.tools.chunked(2).forEach { pair ->
        Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(ColumnGap)) {
            pair.forEach { KubeToolRow(it, labelStyle, Modifier.weight(1f).fillMaxHeight()) }
            if (pair.size == 1) Spacer(Modifier.weight(1f))
        }
    }
}

/** A 48 dp minimum row that opens [tool]'s screen; the label wraps at large text sizes. */
@Composable
private fun KubeToolRow(tool: KubeTool, labelStyle: TextStyle, modifier: Modifier) {
    Row(
        modifier
            .heightIn(min = 48.dp)
            .clip(MaterialTheme.shapes.medium)
            .clickable(role = Role.Button, onClick = tool.onClick)
            .padding(RowPadding),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .size(TileSize)
                .clip(MaterialTheme.shapes.small)
                .background(MaterialTheme.colorScheme.secondaryContainer),
            contentAlignment = Alignment.Center,
        ) {
            Icon(tool.icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSecondaryContainer, modifier = Modifier.size(20.dp))
        }
        Spacer(Modifier.width(TileGap))
        Text(tool.label, style = labelStyle, color = MaterialTheme.colorScheme.onSurface, modifier = Modifier.weight(1f))
        Spacer(Modifier.width(RowPadding))
        Icon(
            Icons.AutoMirrored.Outlined.KeyboardArrowRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(ChevronSize),
        )
    }
}

/** Every tools-card screen in its family; the conditional ones only when the cluster serves them. */
@Composable
private fun kubeToolGroups(
    nav: KubeHomeNavigation,
    crossplane: Boolean,
    tailscale: Boolean,
    clusterLifecycle: Boolean,
    argoRollouts: Boolean,
): List<KubeToolGroup> = listOf(
    KubeToolGroup(
        stringResource(R.string.kube_tools_group_workloads),
        listOfNotNull(
            KubeTool(stringResource(R.string.overview_action_workloads), Icons.Outlined.Widgets, nav.onWorkloads),
            KubeTool(stringResource(R.string.kube_jobs_title), Icons.Outlined.TaskAlt, nav.onJobs),
            KubeTool(stringResource(R.string.kb_helm_title), Icons.Outlined.Inventory2, nav.onHelm),
            KubeTool(stringResource(R.string.autoscaling_title), Icons.Outlined.Speed, nav.onAutoscaling),
            KubeTool(stringResource(R.string.vm_title), Icons.Outlined.Computer, nav.onVirtualMachines),
            // Only when the cluster serves Argo Rollouts: the read answers "not installed" quickly.
            if (argoRollouts) KubeTool(stringResource(R.string.argo_rollouts_title), Icons.Outlined.Upgrade, nav.onArgoRollouts) else null,
        ),
    ),
    KubeToolGroup(
        stringResource(R.string.kube_tools_group_networking),
        listOfNotNull(
            KubeTool(stringResource(R.string.kube_services_title), Icons.Outlined.Lan, nav.onServices),
            KubeTool(stringResource(R.string.netpol_title), Icons.Outlined.Policy, nav.onNetworkPolicies),
            KubeTool(stringResource(R.string.lb_title), Icons.Outlined.Balance, nav.onLoadBalancing),
            // Only when the cluster runs the Tailscale operator.
            if (tailscale) KubeTool(stringResource(R.string.tailscale_title), Icons.Outlined.VpnLock, nav.onTailscale) else null,
        ),
    ),
    KubeToolGroup(
        stringResource(R.string.kube_tools_group_storage),
        listOf(
            KubeTool(stringResource(R.string.storage_title), Icons.Outlined.Storage, nav.onStorage),
            KubeTool(stringResource(R.string.data_services_title), Icons.Outlined.Dataset) { nav.onDataServices(null) },
        ),
    ),
    KubeToolGroup(
        stringResource(R.string.kube_tools_group_observe),
        listOf(
            KubeTool(stringResource(R.string.kube_events_stream_title), Icons.Outlined.Timeline, nav.onEvents),
            KubeTool(stringResource(R.string.metrics_title), Icons.Outlined.QueryStats, nav.onMetrics),
            KubeTool(stringResource(R.string.checkup_title), Icons.Outlined.HealthAndSafety, nav.onCheckup),
            KubeTool(stringResource(R.string.capacity_title), Icons.Outlined.DataUsage, nav.onCapacity),
            KubeTool(stringResource(R.string.apihealth_title), Icons.Outlined.MonitorHeart, nav.onApiHealth),
            KubeTool(stringResource(R.string.kpol_title), Icons.Outlined.Gavel, nav.onPolicies),
        ),
    ),
    KubeToolGroup(
        stringResource(R.string.kube_tools_group_browse),
        listOfNotNull(
            KubeTool(stringResource(R.string.kb_title), Icons.Outlined.Category, nav.onResources),
            // Only when the cluster runs Crossplane: the read answers "not installed" quickly.
            if (crossplane) KubeTool(stringResource(R.string.crossplane_title), Icons.Outlined.Extension, nav.onCrossplane) else null,
            // Only on a management cluster running Cluster API or Sidero Metal.
            if (clusterLifecycle) KubeTool(stringResource(R.string.cluster_lifecycle_title), Icons.Outlined.AccountTree, nav.onClusterLifecycle) else null,
        ),
    ),
)
