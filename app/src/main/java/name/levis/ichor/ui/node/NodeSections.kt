package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Box
import name.levis.ichor.ui.components.smokeMenuTag
import androidx.compose.ui.platform.testTag
import androidx.annotation.StringRes
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
import androidx.compose.material.icons.outlined.Power
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
import name.levis.ichor.model.NodeReachability
import name.levis.ichor.model.down
import name.levis.ichor.model.notice
import name.levis.ichor.model.support
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.text

/** The narrowest a tile gets: four of them across a usual phone, three across a small one. */
private val TILE_MIN_WIDTH = 76.dp
private val TILE_GAP = 8.dp
private const val MIN_COLUMNS = 3
private const val DISABLED_ALPHA = 0.38f

/** Status and Inspect show their entries as one grid of tiles, the other groups as one row each. */
private val NodeMenuGroup.tiled get() = this == NodeMenuGroup.STATUS || this == NodeMenuGroup.INSPECT

/** A section of the node screen's rail: [key] of its title item, its [title], its list [index]. */
data class NodeJump(val key: String, @StringRes val title: Int, val index: Int)

/**
 * The titles [nodeSections] emits for [groups] (and Power when [power]), with their indices in
 * the list; [first] is the index of its first item (the screen's header comes before it);
 * [wake]: Operate ends with the Wake-on-LAN row.
 */
internal fun nodeJumpSections(
    groups: List<Pair<NodeMenuGroup, List<NodeMenuEntry>>>,
    power: Boolean,
    first: Int = 1,
    wake: Boolean = false,
): List<NodeJump> =
    buildList {
        var index = first
        groups.forEach { (group, entries) ->
            add(NodeJump(group.name, group.title, index))
            index += 1 + (if (group.tiled) 1 else entries.size) + (if (wake && group == NodeMenuGroup.OPERATE) 1 else 0)
        }
        if (power) add(NodeJump(POWER_KEY, R.string.node_menu_group_power, index))
    }

private const val POWER_KEY = "power"
private const val WAKE_KEY = "wake"

/**
 * The node screen's sections, as items of its list: Status and Inspect as grids of tiles, the
 * entries that act on the node as rows, the power actions last. [groups] are the ones the role
 * leaves (see [nodeMenuGroups]); [busy] entries are disabled for another reason (e.g. an upgrade
 * elsewhere); [cordoned] is null when unknown; [powerActions] is empty for a role without them.
 * On a node known not to answer ([reachability]), what goes through its Talos API is dimmed
 * with "Node unreachable", the Kubernetes and local entries stay; [onWake] adds the
 * Wake-on-LAN row at the end of Operate.
 */
internal fun LazyListScope.nodeSections(
    groups: List<Pair<NodeMenuGroup, List<NodeMenuEntry>>>,
    features: NodeFeatures?,
    busy: Set<NodeMenuEntry>,
    cordoned: Boolean?,
    powerActions: List<PowerAction>,
    powerEnabled: Boolean,
    reachability: NodeReachability?,
    onPick: (NodeMenuEntry) -> Unit,
    onPower: (PowerAction) -> Unit,
    onWake: (() -> Unit)? = null,
) {
    val kubePods = groups.any { (_, entries) -> NodeMenuEntry.TAB_KUBE_PODS in entries }
    val down = reachability.down
    fun NodeMenuEntry.offline() = down && transport == NodeTransport.TALOS
    groups.forEach { (group, entries) ->
        item(key = group.name) { SectionTitle(stringResource(group.title), Modifier.padding(top = 8.dp)) }
        if (group.tiled) {
            item(key = "${group.name}-tiles") {
                TileGrid(entries) { entry, modifier ->
                    val offline = entry.offline()
                    val notice = entry.notice(features, offline)
                    // A tab this Talos lacks stays reachable, dimmed: it says what it needs.
                    val reachable = !offline && (entry.tab != null || notice == null)
                    MenuTile(
                        label = stringResource(entry.label(cordoned, kubePods)),
                        icon = entry.icon,
                        notice = notice,
                        enabled = entry !in busy && reachable,
                        dimmed = notice != null,
                        onClick = { onPick(entry) },
                        modifier = modifier.testTag(smokeMenuTag(entry)),
                    )
                }
            }
        } else {
            items(entries, key = { it.name }) { entry ->
                val notice = entry.notice(features, entry.offline())
                Box(Modifier.testTag(smokeMenuTag(entry))) {
                    MenuRow(
                        label = stringResource(entry.label(cordoned, kubePods)),
                        icon = entry.icon,
                        notice = notice,
                        enabled = entry !in busy && notice == null,
                        onClick = { onPick(entry) },
                    )
                }
            }
            if (group == NodeMenuGroup.OPERATE && onWake != null) {
                item(key = WAKE_KEY) {
                    MenuRow(label = stringResource(R.string.wol_wake), icon = Icons.Outlined.Power, notice = null, enabled = true, onClick = onWake)
                }
            }
        }
    }
    if (powerActions.isNotEmpty()) {
        item(key = POWER_KEY) { SectionTitle(stringResource(R.string.node_menu_group_power), Modifier.padding(top = 8.dp)) }
        items(powerActions, key = { it.name }) { action ->
            MenuRow(
                label = stringResource(action.title),
                icon = Icons.Outlined.PowerSettingsNew,
                notice = if (down) stringResource(R.string.node_hub_unreachable_notice) else null,
                enabled = powerEnabled && !down,
                onClick = { onPower(action) },
                color = MaterialTheme.colorScheme.error,
            )
        }
    }
}

/**
 * "Node unreachable" when the entry needs a node that does not answer ([offline]; the Talos
 * version is moot then), else "Needs Talos vX or newer" when its Talos lacks what it [NodeMenuEntry.needs].
 */
@Composable
private fun NodeMenuEntry.notice(features: NodeFeatures?, offline: Boolean): String? =
    if (offline) {
        stringResource(R.string.node_hub_unreachable_notice)
    } else {
        (needs?.let { features.support(it) } ?: FeatureSupport.UNKNOWN).notice?.text()
    }

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
