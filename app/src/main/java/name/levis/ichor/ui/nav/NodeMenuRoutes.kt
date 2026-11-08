package name.levis.ichor.ui.nav

import name.levis.ichor.ui.node.NodeMenuEntry

/**
 * The route a node screen's section [entry] opens for the node [addr] ([host]); null for the
 * entries the node screen handles itself (the tabs through onTab, the others a confirmation).
 * Shared by the full-screen node route and the nodes list's detail pane.
 */
internal fun nodeMenuRoute(addr: String, host: String, entry: NodeMenuEntry): String? = when (entry) {
    NodeMenuEntry.KERNEL_LOG -> Routes.logs(addr, host, null)
    NodeMenuEntry.EVENTS -> Routes.events(addr, host)
    NodeMenuEntry.NETWORK -> Routes.network(addr, host)
    NodeMenuEntry.HARDWARE -> Routes.hardware(addr, host)
    NodeMenuEntry.IMAGES -> Routes.images(addr, host)
    NodeMenuEntry.STORAGE -> Routes.storage(addr, host)
    NodeMenuEntry.RESOURCES -> Routes.resources(addr, host)
    NodeMenuEntry.DEBUG_SHELL -> Routes.debug(addr, host)
    NodeMenuEntry.CAPTURE -> Routes.capture(addr, host)
    NodeMenuEntry.NET_TOOLS -> Routes.netTools(addr, host)
    NodeMenuEntry.CAPTURES -> Routes.CAPTURES
    NodeMenuEntry.MACHINE_CONFIG -> Routes.machineConfig(addr, host)
    NodeMenuEntry.UPGRADE -> Routes.upgrade(addr, host)
    NodeMenuEntry.MAINTENANCE -> Routes.maintenance(addr, host)
    NodeMenuEntry.DRAIN -> Routes.maintenance(addr, host, drain = true)
    NodeMenuEntry.TAINTS_LABELS -> Routes.nodeMeta(addr, host)
    else -> null
}
