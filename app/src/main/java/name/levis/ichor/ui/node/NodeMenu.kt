package name.levis.ichor.ui.node

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.NetworkCheck
import androidx.compose.material.icons.outlined.SdStorage
import androidx.compose.material.icons.outlined.SystemUpdateAlt
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.FeatureSupport
import name.levis.ichor.model.NodeFeatures
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.allows
import name.levis.ichor.model.support
import name.levis.ichor.ui.components.FeatureMenuItem

/**
 * Screens of the node's overflow menu. An entry whose [role] the talosconfig lacks is not
 * offered; one whose [needs] the node's Talos version lacks is shown disabled, with the
 * version it needs.
 */
enum class NodeMenuEntry(@StringRes val label: Int, val needs: TalosFeature?, val role: Feature?) {
    KERNEL_LOG(R.string.node_menu_kernel_log, null, null),
    EVENTS(R.string.node_menu_events, TalosFeature.EVENTS, null),
    NETWORK(R.string.node_menu_network, TalosFeature.NETWORK, null),
    HARDWARE(R.string.node_menu_hardware, TalosFeature.HARDWARE, null),
    IMAGES(R.string.node_menu_images, TalosFeature.IMAGES, null),
    STORAGE(R.string.node_menu_storage, TalosFeature.MOUNTS, null),
    RESOURCES(R.string.node_menu_resources, TalosFeature.RESOURCE_BROWSER, Feature.RESOURCE_BROWSER),
    DEBUG_SHELL(R.string.node_menu_debug_shell, TalosFeature.DEBUG_SHELL, Feature.DEBUG_SHELL),
    CAPTURE(R.string.node_menu_capture, TalosFeature.PACKET_CAPTURE, Feature.PACKET_CAPTURE),
    // Saved captures are local files: no Talos version involved.
    CAPTURES(R.string.node_menu_captures, null, Feature.PACKET_CAPTURE),
    MACHINE_CONFIG(R.string.node_menu_machine_config, TalosFeature.MACHINE_CONFIG, Feature.MACHINE_CONFIG),
    UPGRADE(R.string.node_menu_upgrade, TalosFeature.UPGRADE, Feature.UPGRADE),
}

private val NodeMenuEntry.icon: ImageVector
    get() = when (this) {
        NodeMenuEntry.KERNEL_LOG, NodeMenuEntry.DEBUG_SHELL -> Icons.Outlined.Terminal
        NodeMenuEntry.EVENTS -> Icons.Outlined.Timeline
        NodeMenuEntry.NETWORK -> Icons.Outlined.Lan
        NodeMenuEntry.HARDWARE -> Icons.Outlined.Memory
        NodeMenuEntry.IMAGES -> Icons.Outlined.Layers
        NodeMenuEntry.STORAGE -> Icons.Outlined.SdStorage
        NodeMenuEntry.RESOURCES -> Icons.Outlined.AccountTree
        NodeMenuEntry.CAPTURE -> Icons.Outlined.NetworkCheck
        NodeMenuEntry.CAPTURES -> Icons.Outlined.FolderOpen
        NodeMenuEntry.MACHINE_CONFIG -> Icons.Outlined.Description
        NodeMenuEntry.UPGRADE -> Icons.Outlined.SystemUpdateAlt
    }

/** Entries the talosconfig's roles allow, in menu order. */
fun nodeMenuEntries(summary: ContextSummary?): List<NodeMenuEntry> =
    NodeMenuEntry.entries.filter { entry -> entry.role == null || summary?.allows(entry.role) == true }

/** The items of the node menu; [busy] entries are disabled for another reason (e.g. an upgrade elsewhere). */
@Composable
fun NodeMenuItems(
    summary: ContextSummary?,
    features: NodeFeatures?,
    busy: Set<NodeMenuEntry>,
    onPick: (NodeMenuEntry) -> Unit,
) {
    nodeMenuEntries(summary).forEach { entry ->
        FeatureMenuItem(
            label = stringResource(entry.label),
            icon = entry.icon,
            support = entry.needs?.let { features.support(it) } ?: FeatureSupport.UNKNOWN,
            enabled = entry !in busy,
            onClick = { onPick(entry) },
        )
    }
}
