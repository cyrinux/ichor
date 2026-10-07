package name.levis.ichor.ui.node

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.Build
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
import androidx.compose.ui.graphics.vector.ImageVector
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.allows

/** Sections of the node's menu: the screens that only read first, what changes the node last. */
enum class NodeMenuGroup(@StringRes val title: Int) {
    INSPECT(R.string.node_menu_group_inspect),
    TROUBLESHOOT(R.string.node_menu_group_troubleshoot),
    OPERATE(R.string.node_menu_group_operate),
}

/**
 * Entries of the node's menu, by [group]. An entry whose [role] the talosconfig lacks is not
 * offered; one whose [needs] the node's Talos version lacks is shown disabled, with the
 * version it needs.
 */
enum class NodeMenuEntry(val group: NodeMenuGroup, @StringRes val label: Int, val needs: TalosFeature?, val role: Feature?) {
    KERNEL_LOG(NodeMenuGroup.INSPECT, R.string.node_menu_kernel_log, null, null),
    EVENTS(NodeMenuGroup.INSPECT, R.string.node_menu_events, TalosFeature.EVENTS, null),
    NETWORK(NodeMenuGroup.INSPECT, R.string.node_menu_network, TalosFeature.NETWORK, null),
    HARDWARE(NodeMenuGroup.INSPECT, R.string.node_menu_hardware, TalosFeature.HARDWARE, null),
    IMAGES(NodeMenuGroup.INSPECT, R.string.node_menu_images, TalosFeature.IMAGES, null),
    STORAGE(NodeMenuGroup.INSPECT, R.string.node_menu_storage, TalosFeature.MOUNTS, null),
    RESOURCES(NodeMenuGroup.INSPECT, R.string.node_menu_resources, TalosFeature.RESOURCE_BROWSER, Feature.RESOURCE_BROWSER),
    DEBUG_SHELL(NodeMenuGroup.TROUBLESHOOT, R.string.node_menu_debug_shell, TalosFeature.DEBUG_SHELL, Feature.DEBUG_SHELL),
    CAPTURE(NodeMenuGroup.TROUBLESHOOT, R.string.node_menu_capture, TalosFeature.PACKET_CAPTURE, Feature.PACKET_CAPTURE),
    // Saved captures are local files: no Talos version involved.
    CAPTURES(NodeMenuGroup.TROUBLESHOOT, R.string.node_menu_captures, null, Feature.PACKET_CAPTURE),
    MACHINE_CONFIG(NodeMenuGroup.OPERATE, R.string.node_menu_machine_config, TalosFeature.MACHINE_CONFIG, Feature.MACHINE_CONFIG),
    UPGRADE(NodeMenuGroup.OPERATE, R.string.node_menu_upgrade, TalosFeature.UPGRADE, Feature.UPGRADE),
    // Kubernetes calls with the admin kubeconfig (the reboot step needs less): os:admin.
    MAINTENANCE(NodeMenuGroup.OPERATE, R.string.node_menu_maintenance, null, Feature.WORKLOADS),
    // The maintenance screen in drain-only mode: no reboot or shutdown.
    DRAIN(NodeMenuGroup.OPERATE, R.string.node_menu_drain, null, Feature.WORKLOADS),
    // Labelled Uncordon when the node is known to be cordoned (see NodeMenuSheet).
    CORDON(NodeMenuGroup.OPERATE, R.string.node_menu_cordon, null, Feature.WORKLOADS),
}

internal val NodeMenuEntry.icon: ImageVector
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
        NodeMenuEntry.MAINTENANCE -> Icons.Outlined.Build
        NodeMenuEntry.DRAIN -> Icons.AutoMirrored.Outlined.Logout
        NodeMenuEntry.CORDON -> Icons.Outlined.Block
    }

/** Entries the talosconfig's roles allow, in menu order. */
fun nodeMenuEntries(summary: ContextSummary?): List<NodeMenuEntry> =
    NodeMenuEntry.entries.filter { entry -> entry.role == null || summary?.allows(entry.role) == true }

/** The groups the roles leave an entry in, each with its entries, in menu order. */
fun nodeMenuGroups(summary: ContextSummary?): List<Pair<NodeMenuGroup, List<NodeMenuEntry>>> =
    NodeMenuGroup.entries.mapNotNull { group ->
        nodeMenuEntries(summary).filter { it.group == group }.takeIf { it.isNotEmpty() }?.let { group to it }
    }
