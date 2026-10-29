import Foundation

/// One entry of the cluster title menu (both homes).
public enum ClusterMenuItem: Hashable, Sendable {
    case switchCluster, fleet, manage
    case upgradeCluster, upgradeKubernetes, addNode, updates, runbooks
    case incidentSummary, shareLink
}

/// The cluster title menu by intent, in order: which cluster, operate it, share it. A group with
/// no item is left out, so the menu never shows two dividers in a row. Same groups as Android
/// (`clusterMenuGroups` in ClusterMenuItems.kt). `context` is the active cluster (nil: none yet);
/// `canSwitch` is false with a single cluster (no switch, no "All clusters"). A kubeconfig cluster has no Talos API: no
/// upgrades, no Add a node; on Omni, Add a node opens the boot media. Updates (the images and
/// charts with a newer version) needs an active cluster.
public func clusterMenuGroups(context: ContextSummary?, canSwitch: Bool) -> [[ClusterMenuItem]] {
    let talos = context.map { !$0.isKube } ?? false
    let upgrade = context?.allows(.upgrade) ?? false
    let clusters: [ClusterMenuItem] = (canSwitch ? [.switchCluster, .fleet] : []) + [.manage]
    let operate: [ClusterMenuItem] =
        (upgrade ? [.upgradeCluster, .upgradeKubernetes] : [])
        + (talos ? [.addNode] : []) + (context != nil ? [.updates] : []) + [.runbooks]
    let share: [ClusterMenuItem] = (context != nil ? [.incidentSummary] : []) + [.shareLink]
    return [clusters, operate, share].filter { !$0.isEmpty }
}
