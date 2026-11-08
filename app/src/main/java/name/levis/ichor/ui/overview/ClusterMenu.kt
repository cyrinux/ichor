package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import name.levis.ichor.ui.components.SMOKE_CLUSTER_UPGRADE
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.ShareLinkMenuItem
import name.levis.ichor.ui.share.AddToHomeMenuItem
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.SystemUpdateAlt
import androidx.compose.material.icons.outlined.Update
import androidx.compose.material.icons.outlined.Upgrade
import androidx.compose.material.icons.outlined.SwapHoriz
import androidx.compose.material.icons.outlined.ViewAgenda
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.annotation.StringRes
import androidx.compose.ui.graphics.Color
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ClusterMenuItem
import name.levis.ichor.model.clusterMenuGroups
import name.levis.ichor.model.clusterSwitchGroups
import name.levis.ichor.model.seedOf
import name.levis.ichor.model.accessLabel
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.ui.nav.LocalClusterRail
import name.levis.ichor.ui.components.ClusterLogo
import name.levis.ichor.ui.incident.IncidentSummarySheet

/**
 * The overview title's menu, as on iOS: the clusters behind a "Switch cluster" submenu, so
 * "Manage clusters…" stays in reach without a scroll however many clusters there are.
 * Compose menus do not nest, so the submenu takes the menu's place, with a way back.
 * "Share incident summary…" opens its sheet from here, so every cluster home gets it.
 * Grouped by intent ([clusterMenuGroups]): which cluster, operate it, share it.
 */
@Composable
fun ClusterMenu(
    expanded: Boolean,
    config: StoredConfig,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    onSelect: (String) -> Unit,
    onManage: () -> Unit,
    /** "All clusters": every cluster's last check on one list, worst first. */
    onFleet: () -> Unit,
    onDismiss: () -> Unit,
    /** "Upgrade cluster…": shown when the role can upgrade a Talos cluster. */
    onUpgradeCluster: () -> Unit = {},
    /** "Upgrade Kubernetes…": shown when the role can upgrade a Talos cluster. */
    onUpgradeKubernetes: () -> Unit = {},
    /** "Add a node…": shown for a cluster with the Talos API (on Omni it opens the boot media). */
    onAddNode: () -> Unit = {},
    /** "Runbooks…": the cluster's own ordered lists of actions. */
    onRunbooks: () -> Unit,
    /** "Updates…": the images and charts with a newer version. */
    onUpdates: () -> Unit,
) {
    var switching by remember(expanded) { mutableStateOf(false) }
    var expandedCluster by remember(expanded) { mutableStateOf<String?>(null) }
    var incident by remember { mutableStateOf(false) }
    val contexts = config.summary.contexts
    val current = config.activeSummary
    if (incident && current != null) IncidentSummarySheet(current, onDismiss = { incident = false })
    DropdownMenu(expanded = expanded, onDismissRequest = onDismiss) {
        if (switching) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.clusters_switch)) },
                leadingIcon = { Icon(Icons.AutoMirrored.Outlined.ArrowBack, stringResource(R.string.common_back)) },
                onClick = { switching = false },
            )
            HorizontalDivider()
            clusterSwitchGroups(contexts).forEach { group ->
                if (group.size == 1) {
                    val context = group.first()
                    ClusterChoice(context, labels.of(context), context.name == config.activeContext, colors, onDismiss) { onSelect(context.name) }
                } else {
                    val representative = group.first()
                    val key = representative.clusterId.ifBlank { representative.fingerprint.ifBlank { representative.name } }
                    val selected = group.firstOrNull { it.name == config.activeContext }
                    DropdownMenuItem(
                        text = {
                            Column {
                                Text(clusterSwitchTitle(group, labels), maxLines = 1, overflow = TextOverflow.Ellipsis)
                                Text(
                                    listOfNotNull(
                                        stringResource(R.string.clusters_access_profiles, group.size),
                                        selected?.let { stringResource(it.accessLabel) },
                                    ).joinToString(" · "),
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    style = MaterialTheme.typography.bodySmall,
                                )
                            }
                        },
                        leadingIcon = { ClusterLogo(representative, Color(colors.seedOf(representative)), selected = false, size = 24.dp) },
                        trailingIcon = {
                            Icon(
                                if (expandedCluster == key) Icons.Outlined.KeyboardArrowDown else Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                                contentDescription = null,
                            )
                        },
                        onClick = { expandedCluster = if (expandedCluster == key) null else key },
                    )
                    if (expandedCluster == key) group.forEach { context ->
                        ClusterChoice(
                            context,
                            labels.of(context),
                            context.name == config.activeContext,
                            colors,
                            onDismiss,
                            supporting = stringResource(context.accessLabel),
                            modifier = Modifier.padding(start = 24.dp),
                        ) { onSelect(context.name) }
                    }
                }
            }
        } else {
            // On a tablet the rail lists the clusters: one way to switch, not two.
            val groups = clusterMenuGroups(current, canSwitch = contexts.size > 1 && !LocalClusterRail.current)
            groups.forEachIndexed { index, group ->
                if (index > 0) HorizontalDivider()
                group.forEach { item ->
                    when (item) {
                        ClusterMenuItem.SWITCH -> DropdownMenuItem(
                            text = { Text(stringResource(R.string.clusters_switch)) },
                            leadingIcon = { Icon(Icons.Outlined.SwapHoriz, contentDescription = null) },
                            trailingIcon = { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null) },
                            onClick = { switching = true },
                        )
                        ClusterMenuItem.FLEET -> Entry(R.string.fleet_title, Icons.Outlined.ViewAgenda, onDismiss, onFleet)
                        ClusterMenuItem.MANAGE -> Entry(R.string.clusters_manage, Icons.Outlined.Layers, onDismiss, onManage)
                        ClusterMenuItem.UPGRADE_CLUSTER -> Box(Modifier.testTag(SMOKE_CLUSTER_UPGRADE)) { Entry(R.string.cluster_upgrade_menu, Icons.Outlined.SystemUpdateAlt, onDismiss, onUpgradeCluster) }
                        ClusterMenuItem.UPGRADE_KUBERNETES -> Entry(R.string.k8s_upgrade_menu, Icons.Outlined.Upgrade, onDismiss, onUpgradeKubernetes)
                        ClusterMenuItem.ADD_NODE -> Entry(R.string.join_node_menu, Icons.Outlined.AddCircleOutline, onDismiss, onAddNode)
                        ClusterMenuItem.UPDATES -> Entry(R.string.updates_menu, Icons.Outlined.Update, onDismiss, onUpdates)
                        ClusterMenuItem.RUNBOOKS -> Entry(R.string.runbooks_menu, Icons.Outlined.Checklist, onDismiss, onRunbooks)
                        ClusterMenuItem.INCIDENT_SUMMARY -> Entry(R.string.incident_summary_menu, Icons.Outlined.Share, onDismiss) { incident = true }
                        ClusterMenuItem.SHARE_LINK -> ShareLinkMenuItem(ShareTarget.screen(ShareTarget.CLUSTER), onClick = onDismiss)
                        // No label: the cluster names the shortcut.
                        ClusterMenuItem.ADD_TO_HOME -> AddToHomeMenuItem(ShareTarget.screen(ShareTarget.CLUSTER), label = "", onClick = onDismiss)
                    }
                }
            }
        }
    }
}

@Composable
private fun clusterSwitchTitle(group: List<ContextSummary>, labels: ClusterLabels): String {
    val named = group.firstOrNull { labels.given(it) != null }
        ?: group.firstOrNull { it.name.lowercase() !in setOf("admin", "operator", "reader") }
    if (named != null) return labels.of(named)
    val endpoint = group.first().endpoints.firstOrNull().takeUnless { labels.masked }
    return endpoint?.let { stringResource(R.string.clusters_at, it) }
        ?: stringResource(R.string.clusters_unnamed_group)
}

@Composable
private fun ClusterChoice(
    context: ContextSummary,
    title: String,
    active: Boolean,
    colors: Map<String, Int>,
    onDismiss: () -> Unit,
    supporting: String? = null,
    modifier: Modifier = Modifier,
    onSelect: () -> Unit,
) {
    DropdownMenuItem(
        modifier = modifier,
        text = {
            Column {
                Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                supporting?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
            }
        },
        leadingIcon = { ClusterLogo(context, Color(colors.seedOf(context)), selected = false, size = 24.dp) },
        trailingIcon = if (active) { { Icon(Icons.Outlined.Check, contentDescription = null) } } else null,
        onClick = {
            onDismiss()
            if (!active) onSelect()
        },
    )
}

/** A menu item that closes the menu, then runs [onClick]. */
@Composable
private fun Entry(@StringRes label: Int, icon: ImageVector, onDismiss: () -> Unit, onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(stringResource(label)) },
        leadingIcon = { Icon(icon, contentDescription = null) },
        onClick = {
            onDismiss()
            onClick()
        },
    )
}
