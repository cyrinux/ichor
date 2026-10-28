package name.levis.ichor.model

/** One entry of the cluster title menu (both homes). */
enum class ClusterMenuItem {
    SWITCH,
    MANAGE,
    UPGRADE_CLUSTER,
    UPGRADE_KUBERNETES,
    ADD_NODE,
    RUNBOOKS,
    INCIDENT_SUMMARY,
    SHARE_LINK,
    ADD_TO_HOME,
}

/**
 * The cluster title menu by intent, in order: which cluster, operate it, share it. A group with
 * no item is left out, so the menu never shows two dividers in a row. Same groups as iOS
 * (`clusterMenuGroups` in IchorCore). [context] is the active cluster (null: none yet);
 * [canSwitch] is false with a single cluster or when the tablet rail lists them. A kubeconfig
 * cluster has no Talos API: no upgrades, no Add a node; on Omni, Add a node opens the boot media.
 */
fun clusterMenuGroups(context: ContextSummary?, canSwitch: Boolean): List<List<ClusterMenuItem>> {
    val talos = context != null && !context.isKube
    val upgrade = context?.allows(Feature.UPGRADE) == true
    val clusters = listOfNotNull(ClusterMenuItem.SWITCH.takeIf { canSwitch }, ClusterMenuItem.MANAGE)
    val operate = listOfNotNull(
        ClusterMenuItem.UPGRADE_CLUSTER.takeIf { upgrade },
        ClusterMenuItem.UPGRADE_KUBERNETES.takeIf { upgrade },
        ClusterMenuItem.ADD_NODE.takeIf { talos },
        ClusterMenuItem.RUNBOOKS,
    )
    val share = listOfNotNull(
        ClusterMenuItem.INCIDENT_SUMMARY.takeIf { context != null },
        ClusterMenuItem.SHARE_LINK,
        ClusterMenuItem.ADD_TO_HOME,
    )
    return listOf(clusters, operate, share).filter { it.isNotEmpty() }
}
