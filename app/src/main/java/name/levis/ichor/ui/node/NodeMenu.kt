package name.levis.ichor.ui.node

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ListAlt
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.automirrored.outlined.ShowChart
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.Build
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.MiscellaneousServices
import androidx.compose.material.icons.outlined.NetworkCheck
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.SdStorage
import androidx.compose.material.icons.outlined.Sell
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.SystemUpdateAlt
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.ui.graphics.vector.ImageVector
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.allows

/**
 * Sections of the node screen: the node's state first (its tabs), then the screens that only
 * read, what changes the node last.
 */
enum class NodeMenuGroup(@StringRes val title: Int) {
    STATUS(R.string.node_menu_group_status),
    INSPECT(R.string.node_menu_group_inspect),
    TROUBLESHOOT(R.string.node_menu_group_troubleshoot),
    OPERATE(R.string.node_menu_group_operate),
}

/**
 * Entries of the node screen, by [group]. An entry whose [role] the talosconfig lacks is not
 * offered; one whose [needs] the node's Talos version lacks is shown disabled, with the
 * version it needs. A Status entry opens the node's tabs on its [tab]; it stays enabled (dimmed)
 * when the Talos version lacks it, as the tab says what it needs.
 */
enum class NodeMenuEntry(
    val group: NodeMenuGroup,
    @StringRes val label: Int,
    val needs: TalosFeature?,
    val role: Feature?,
    val tab: Int? = null,
) {
    TAB_SERVICES(NodeMenuGroup.STATUS, R.string.node_tab_services, null, null, tab = 0),
    TAB_RESOURCES(NodeMenuGroup.STATUS, R.string.node_tab_resources, null, null, tab = 1),
    TAB_LIVE(NodeMenuGroup.STATUS, R.string.node_tab_live, null, null, tab = 2),
    TAB_PROCESSES(NodeMenuGroup.STATUS, R.string.node_tab_processes, TalosFeature.PROCESSES, null, tab = 3),
    // Named Containers next to Kubernetes pods (see label()).
    TAB_CONTAINERS(NodeMenuGroup.STATUS, R.string.node_tab_pods, TalosFeature.CONTAINERS, null, tab = 4),
    // Pressure and cgroups come from a copy of /sys/fs/cgroup: os:admin only.
    TAB_CGROUPS(NodeMenuGroup.STATUS, R.string.node_tab_cgroups, null, Feature.CGROUPS, tab = CGROUPS_TAB),
    // The node's pods as the Kubernetes API sees them: os:admin only.
    TAB_KUBE_PODS(NodeMenuGroup.STATUS, R.string.node_tab_kube_pods, null, Feature.WORKLOADS, tab = KUBE_PODS_TAB),
    KERNEL_LOG(NodeMenuGroup.INSPECT, R.string.node_menu_kernel_log, null, null),
    EVENTS(NodeMenuGroup.INSPECT, R.string.node_menu_events, TalosFeature.EVENTS, null),
    NETWORK(NodeMenuGroup.INSPECT, R.string.node_menu_network, TalosFeature.NETWORK, null),
    HARDWARE(NodeMenuGroup.INSPECT, R.string.node_menu_hardware, TalosFeature.HARDWARE, null),
    IMAGES(NodeMenuGroup.INSPECT, R.string.node_menu_images, TalosFeature.IMAGES, null),
    STORAGE(NodeMenuGroup.INSPECT, R.string.node_menu_storage, TalosFeature.MOUNTS, null),
    RESOURCES(NodeMenuGroup.INSPECT, R.string.node_menu_resources, TalosFeature.RESOURCE_BROWSER, Feature.RESOURCE_BROWSER),
    DEBUG_SHELL(NodeMenuGroup.TROUBLESHOOT, R.string.node_menu_debug_shell, TalosFeature.DEBUG_SHELL, Feature.DEBUG_SHELL),
    // The same privileged netshoot container as the debug shell, one check at a time.
    NET_TOOLS(NodeMenuGroup.TROUBLESHOOT, R.string.node_menu_net_tools, TalosFeature.DEBUG_SHELL, Feature.DEBUG_SHELL),
    CAPTURE(NodeMenuGroup.TROUBLESHOOT, R.string.node_menu_capture, TalosFeature.PACKET_CAPTURE, Feature.PACKET_CAPTURE),
    // Saved captures are local files: no Talos version involved.
    CAPTURES(NodeMenuGroup.TROUBLESHOOT, R.string.node_menu_captures, null, Feature.PACKET_CAPTURE),
    MACHINE_CONFIG(NodeMenuGroup.OPERATE, R.string.node_menu_machine_config, TalosFeature.MACHINE_CONFIG, Feature.MACHINE_CONFIG),
    UPGRADE(NodeMenuGroup.OPERATE, R.string.node_menu_upgrade, TalosFeature.UPGRADE, Feature.UPGRADE),
    // Handled on the node screen: a plan and a confirmation, no screen of its own.
    RESET(NodeMenuGroup.OPERATE, R.string.node_menu_reset, TalosFeature.RESET, Feature.RESET),
    // Kubernetes calls with the admin kubeconfig (the reboot step needs less): os:admin.
    MAINTENANCE(NodeMenuGroup.OPERATE, R.string.node_menu_maintenance, null, Feature.WORKLOADS),
    // The maintenance screen in drain-only mode: no reboot or shutdown.
    DRAIN(NodeMenuGroup.OPERATE, R.string.node_menu_drain, null, Feature.WORKLOADS),
    // Labelled Uncordon when the node is known to be cordoned (see label()).
    CORDON(NodeMenuGroup.OPERATE, R.string.node_menu_cordon, null, Feature.WORKLOADS),
    // The node's taints and labels through the Kubernetes API, like the cordon: os:admin.
    TAINTS_LABELS(NodeMenuGroup.OPERATE, R.string.node_meta_menu, null, Feature.WORKLOADS),
}

internal val NodeMenuEntry.icon: ImageVector
    get() = when (this) {
        NodeMenuEntry.TAB_SERVICES -> Icons.Outlined.MiscellaneousServices
        NodeMenuEntry.TAB_RESOURCES -> Icons.Outlined.Speed
        NodeMenuEntry.TAB_LIVE -> Icons.AutoMirrored.Outlined.ShowChart
        NodeMenuEntry.TAB_PROCESSES -> Icons.AutoMirrored.Outlined.ListAlt
        NodeMenuEntry.TAB_CONTAINERS -> Icons.Outlined.Inventory2
        NodeMenuEntry.TAB_CGROUPS -> Icons.Outlined.Tune
        NodeMenuEntry.TAB_KUBE_PODS -> Icons.Outlined.ViewInAr
        NodeMenuEntry.KERNEL_LOG, NodeMenuEntry.DEBUG_SHELL -> Icons.Outlined.Terminal
        NodeMenuEntry.EVENTS -> Icons.Outlined.Timeline
        NodeMenuEntry.NETWORK -> Icons.Outlined.Lan
        NodeMenuEntry.HARDWARE -> Icons.Outlined.Memory
        NodeMenuEntry.IMAGES -> Icons.Outlined.Layers
        NodeMenuEntry.STORAGE -> Icons.Outlined.SdStorage
        NodeMenuEntry.RESOURCES -> Icons.Outlined.AccountTree
        NodeMenuEntry.CAPTURE -> Icons.Outlined.NetworkCheck
        NodeMenuEntry.NET_TOOLS -> Icons.Outlined.Dns
        NodeMenuEntry.CAPTURES -> Icons.Outlined.FolderOpen
        NodeMenuEntry.MACHINE_CONFIG -> Icons.Outlined.Description
        NodeMenuEntry.UPGRADE -> Icons.Outlined.SystemUpdateAlt
        NodeMenuEntry.RESET -> Icons.Outlined.RestartAlt
        NodeMenuEntry.MAINTENANCE -> Icons.Outlined.Build
        NodeMenuEntry.DRAIN -> Icons.AutoMirrored.Outlined.Logout
        NodeMenuEntry.CORDON -> Icons.Outlined.Block
        NodeMenuEntry.TAINTS_LABELS -> Icons.Outlined.Sell
    }

/**
 * The label on screen: Uncordon for a node known to be [cordoned], Containers for the Talos (CRI)
 * view when the Kubernetes pods entry sits next to it ([kubePods]).
 */
@StringRes
internal fun NodeMenuEntry.label(cordoned: Boolean?, kubePods: Boolean): Int = when {
    this == NodeMenuEntry.CORDON && cordoned == true -> R.string.node_menu_uncordon
    this == NodeMenuEntry.TAB_CONTAINERS && kubePods -> R.string.apps_stat_containers
    else -> label
}

/** Entries the talosconfig's roles allow, in screen order. */
fun nodeMenuEntries(summary: ContextSummary?): List<NodeMenuEntry> =
    NodeMenuEntry.entries.filter { entry -> entry.role == null || summary?.allows(entry.role) == true }

/** The groups the roles leave an entry in, each with its entries, in screen order. */
fun nodeMenuGroups(summary: ContextSummary?): List<Pair<NodeMenuGroup, List<NodeMenuEntry>>> =
    NodeMenuGroup.entries.mapNotNull { group ->
        nodeMenuEntries(summary).filter { it.group == group }.takeIf { it.isNotEmpty() }?.let { group to it }
    }
