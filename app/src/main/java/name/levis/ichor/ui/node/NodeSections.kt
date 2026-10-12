package name.levis.ichor.ui.node

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FeatureSupport
import name.levis.ichor.model.NodeFeatures
import name.levis.ichor.model.notice
import name.levis.ichor.model.support
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.text

/** The narrowest a tile gets: four of them across a usual phone, three across a small one. */
private val TILE_MIN_WIDTH = 76.dp
private val TILE_GAP = 8.dp
private const val MIN_COLUMNS = 3
private const val DISABLED_ALPHA = 0.38f

/**
 * The node screen's sections, as items of its list: Status and Inspect as grids of tiles, the
 * entries that act on the node as rows, the power actions last. [groups] are the ones the role
 * leaves (see [nodeMenuGroups]); [busy] entries are disabled for another reason (e.g. an upgrade
 * elsewhere); [cordoned] is null when unknown; [powerActions] is empty for a role without them.
 */
internal fun LazyListScope.nodeSections(
    groups: List<Pair<NodeMenuGroup, List<NodeMenuEntry>>>,
    features: NodeFeatures?,
    busy: Set<NodeMenuEntry>,
    cordoned: Boolean?,
    powerActions: List<PowerAction>,
    powerEnabled: Boolean,
    onPick: (NodeMenuEntry) -> Unit,
    onPower: (PowerAction) -> Unit,
) {
    val kubePods = groups.any { (_, entries) -> NodeMenuEntry.TAB_KUBE_PODS in entries }
    groups.forEach { (group, entries) ->
        item(key = group.name) { SectionTitle(stringResource(group.title), Modifier.padding(top = 8.dp)) }
        if (group == NodeMenuGroup.STATUS || group == NodeMenuGroup.INSPECT) {
            item(key = "${group.name}-tiles") {
                TileGrid(entries) { entry, modifier ->
                    val notice = entry.notice(features)
                    // A tab this Talos lacks stays reachable, dimmed: it says what it needs.
                    val reachable = entry.tab != null || notice == null
                    MenuTile(
                        label = stringResource(entry.label(cordoned, kubePods)),
                        icon = entry.icon,
                        notice = notice,
                        enabled = entry !in busy && reachable,
                        dimmed = notice != null,
                        onClick = { onPick(entry) },
                        modifier = modifier,
                    )
                }
            }
        } else {
            items(entries, key = { it.name }) { entry ->
                val notice = entry.notice(features)
                MenuRow(
                    label = stringResource(entry.label(cordoned, kubePods)),
                    icon = entry.icon,
                    notice = notice,
                    enabled = entry !in busy && notice == null,
                    onClick = { onPick(entry) },
                )
            }
        }
    }
    if (powerActions.isNotEmpty()) {
        item(key = "power") { SectionTitle(stringResource(R.string.node_menu_group_power), Modifier.padding(top = 8.dp)) }
        items(powerActions, key = { it.name }) { action ->
            MenuRow(
                label = stringResource(action.title),
                icon = Icons.Outlined.PowerSettingsNew,
                notice = null,
                enabled = powerEnabled,
                onClick = { onPower(action) },
                color = MaterialTheme.colorScheme.error,
            )
        }
    }
}

/** "Needs Talos vX or newer" when the node's Talos lacks what the entry [NodeMenuEntry.needs]. */
@Composable
private fun NodeMenuEntry.notice(features: NodeFeatures?): String? =
    (needs?.let { features.support(it) } ?: FeatureSupport.UNKNOWN).notice?.text()

/** [entries] as rows of equal tiles, as many across as the width takes. */
@Composable
private fun TileGrid(entries: List<NodeMenuEntry>, tile: @Composable (NodeMenuEntry, Modifier) -> Unit) {
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val columns = ((maxWidth + TILE_GAP) / (TILE_MIN_WIDTH + TILE_GAP)).toInt().coerceAtLeast(MIN_COLUMNS)
        Column(verticalArrangement = Arrangement.spacedBy(TILE_GAP)) {
            entries.chunked(columns).forEach { row ->
                Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(TILE_GAP)) {
                    row.forEach { tile(it, Modifier.weight(1f).fillMaxHeight()) }
                    repeat(columns - row.size) { Spacer(Modifier.weight(1f)) }
                }
            }
        }
    }
}

@Composable
private fun MenuTile(
    label: String,
    icon: ImageVector,
    notice: String?,
    enabled: Boolean,
    dimmed: Boolean,
    onClick: () -> Unit,
    modifier: Modifier,
) {
    Surface(
        onClick = onClick,
        enabled = enabled,
        shape = MaterialTheme.shapes.medium,
        color = MaterialTheme.colorScheme.surfaceContainerHighest,
        modifier = modifier,
    ) {
        Column(
            Modifier.alpha(if (enabled && !dimmed) 1f else DISABLED_ALPHA).padding(horizontal = 4.dp, vertical = 12.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(icon, contentDescription = null)
            Text(label, style = MaterialTheme.typography.labelMedium, textAlign = TextAlign.Center, maxLines = 2, overflow = TextOverflow.Ellipsis)
            notice?.let { Text(it, style = MaterialTheme.typography.labelSmall, textAlign = TextAlign.Center) }
        }
    }
}

@Composable
private fun MenuRow(
    label: String,
    icon: ImageVector,
    notice: String?,
    enabled: Boolean,
    onClick: () -> Unit,
    color: Color = MaterialTheme.colorScheme.onSurface,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .alpha(if (enabled) 1f else DISABLED_ALPHA)
            .padding(horizontal = 12.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Icon(icon, contentDescription = null, tint = color)
        Column {
            Text(label, style = MaterialTheme.typography.bodyLarge, color = color)
            notice?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = color) }
        }
    }
}
