import Foundation

// Mirrors go/ichorgo/kube_nodegroups_eks_upgrade.go: a group upgraded from the phone
// (UpdateNodegroupVersion), followed like a size change.

/// What an upgrade does: the control plane's Kubernetes version, or the latest node image of
/// the group's own version.
public enum NodeGroupUpgradeChoice: Hashable, Sendable {
    case kubernetes(String)
    case latestImage

    /// The version sent to AWS: "" keeps the group's own and takes its latest node image.
    public var version: String {
        switch self {
        case .kubernetes(let version): version
        case .latestImage: ""
        }
    }
}

extension ManagedNodeGroupRow {
    /// The AMI type of a group with its own image: it changes through its launch template, not here.
    public static let customAMI = "CUSTOM"

    /// A custom AMI: no "latest node image" for it.
    public var customAMI: Bool { amiType == Self.customAMI }

    /// The upgrades the group's sheet offers: the control plane's version when the group is
    /// behind it, and the latest node image unless the AMI is custom; none while the group may
    /// not change.
    public func upgradeChoices(clusterVersion: String) -> [NodeGroupUpgradeChoice] {
        guard canChangeSize else { return [] }
        var choices: [NodeGroupUpgradeChoice] = []
        if behind && !clusterVersion.isEmpty { choices.append(.kubernetes(clusterVersion)) }
        if !customAMI { choices.append(.latestImage) }
        return choices
    }
}

extension NodeGroupUpdate {
    /// AWS' type of an update that replaces the group's nodes (a size change is a ConfigUpdate).
    public static let versionUpdateType = "VersionUpdate"
    /// The update error of a pod AWS could not evict while it replaced a node.
    public static let podEvictionFailureCode = "PodEvictionFailure"

    /// The update replaces the group's nodes (an upgrade), rather than changing its size.
    public var versionUpdate: Bool { type == Self.versionUpdateType }

    /// The pod eviction AWS gave up on, when that is why the update failed.
    public var podEvictionFailure: NodeGroupIssue? { errors.first { $0.code == Self.podEvictionFailureCode } }
}
