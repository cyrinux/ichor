import Foundation

// Mirrors go/ichorgo/kube_eks_upgrade.go: upgrading an EKS control plane one Kubernetes minor
// (UpdateClusterVersion), the plan that says what it will do, and the update AWS runs for it.
// installed false: not an EKS sign-in, the Autoscaling screen shows no control plane row.

/// What upgrading the control plane one minor would do. `next` is empty when AWS offers no newer
/// version; `nextKnown` false with a `next` means availability was not checked (the role may not
/// call DescribeClusterVersions). `supportEnds` is the end of standard support of `version`, unix
/// ms, 0 when unknown. `updating` is the update in progress, found again when the app reopens.
/// `groups` are every managed node group: each is behind once the control plane moves.
public struct EksUpgradePlan: Decodable, Equatable, Hashable, Sendable {
    public let installed: Bool
    public let cluster: String
    public let region: String
    public let version: String
    public let status: String
    public let platformVersion: String
    public let next: String
    public let nextKnown: Bool
    public let supportEnds: Int64
    public let updating: NodeGroupUpdate?
    public let groupsBehind: [String]
    public let groups: [String]
    public let blockers: [String]
    public let error: String
    /// The AWS action the role may not call ("eks:DescribeCluster"), when that is the error.
    public let forbidden: String

    public init(
        installed: Bool = false, cluster: String = "", region: String = "", version: String = "",
        status: String = "", platformVersion: String = "", next: String = "", nextKnown: Bool = false,
        supportEnds: Int64 = 0, updating: NodeGroupUpdate? = nil, groupsBehind: [String] = [],
        groups: [String] = [], blockers: [String] = [], error: String = "", forbidden: String = ""
    ) {
        self.installed = installed
        self.cluster = cluster
        self.region = region
        self.version = version
        self.status = status
        self.platformVersion = platformVersion
        self.next = next
        self.nextKnown = nextKnown
        self.supportEnds = supportEnds
        self.updating = updating
        self.groupsBehind = groupsBehind
        self.groups = groups
        self.blockers = blockers
        self.error = error
        self.forbidden = forbidden
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        cluster = try c.field(.cluster, "")
        region = try c.field(.region, "")
        version = try c.field(.version, "")
        status = try c.field(.status, "")
        platformVersion = try c.field(.platformVersion, "")
        next = try c.field(.next, "")
        nextKnown = try c.field(.nextKnown, false)
        supportEnds = try c.field(.supportEnds, 0)
        updating = try c.decodeIfPresent(NodeGroupUpdate.self, forKey: .updating)
        groupsBehind = try c.field(.groupsBehind, [])
        groups = try c.field(.groups, [])
        blockers = try c.field(.blockers, [])
        error = try c.field(.error, "")
        forbidden = try c.field(.forbidden, "")
    }

    private enum CodingKeys: String, CodingKey {
        case installed, cluster, region, version, status, platformVersion, next, nextKnown, supportEnds, updating
        case groupsBehind, groups, blockers, error, forbidden
    }

    /// The upgrade may start now: a next minor, no blocker, no error and nothing running.
    public var canUpgrade: Bool {
        !next.isEmpty && blockers.isEmpty && error.isEmpty && updating?.inProgress != true
    }

    /// The row's state, `update` (the one the app follows, if any) first.
    public func state(update: NodeGroupUpdate?) -> EksUpgradeState {
        if (update ?? updating)?.inProgress == true { return .running }
        if version.isEmpty { return .unknown }
        if next.isEmpty { return .upToDate }
        return nextKnown ? .available : .unchecked
    }
}

/// What the control plane row says: an update running, a next minor (checked with AWS or not),
/// up to date, or nothing known (the plan's error).
public enum EksUpgradeState: Equatable, Sendable {
    case running, available, unchecked, upToDate, unknown
}
