import Foundation

// Mirrors go/ichorgo/kube_nodegroups_eks.go: a cluster's cloud node pools (EKS managed node
// groups), read with the cluster's own AWS sign-in. installed false: not an EKS sign-in, the
// Autoscaling screen hides the group.

/// The cluster's managed node groups, or why they could not be read.
public struct NodeGroupsReport: Decodable, Equatable, Hashable, Sendable {
    public let provider: String
    public let installed: Bool
    public let region: String
    public let cluster: String
    public let error: String
    /// The AWS action the role may not call ("eks:ListNodegroups"), when that is the error.
    public let forbidden: String
    public let groups: [ManagedNodeGroupRow]

    public init(
        provider: String = "", installed: Bool = false, region: String = "", cluster: String = "",
        error: String = "", forbidden: String = "", groups: [ManagedNodeGroupRow] = []
    ) {
        self.provider = provider
        self.installed = installed
        self.region = region
        self.cluster = cluster
        self.error = error
        self.forbidden = forbidden
        self.groups = groups
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        provider = try c.field(.provider, "")
        installed = try c.field(.installed, false)
        region = try c.field(.region, "")
        cluster = try c.field(.cluster, "")
        error = try c.field(.error, "")
        forbidden = try c.field(.forbidden, "")
        groups = try c.field(.groups, [])
    }

    private enum CodingKeys: String, CodingKey {
        case provider, installed, region, cluster, error, forbidden, groups
    }

    /// The groups a search `query` keeps (name, version, capacity type, status, instance types).
    public func filtered(query: String) -> NodeGroupsReport {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return self }
        return NodeGroupsReport(
            provider: provider, installed: installed, region: region, cluster: cluster, error: error,
            forbidden: forbidden,
            groups: groups.filter { g in
                ([g.name, g.version, g.capacityType, g.status] + g.instanceTypes).contains {
                    $0.localizedCaseInsensitiveContains(q)
                }
            })
    }
}

/// One managed node group: its size against its bounds, version, capacity and AWS' health issues.
public struct ManagedNodeGroupRow: Decodable, Equatable, Hashable, Identifiable, Sendable {
    /// EKS' status of a group that is up and running.
    public static let active = "ACTIVE"

    public let name: String
    public let status: String
    public let desired: Int
    public let min: Int
    public let max: Int
    public let version: String
    public let releaseVersion: String
    public let amiType: String
    public let capacityType: String
    public let instanceTypes: [String]
    public let diskSize: Int
    /// The cluster's nodes carrying the group's label; differs from `desired` while it scales.
    public let nodes: Int
    public let atMax: Bool
    public let issues: [NodeGroupIssue]
    public let updating: Bool
    /// Unix ms, 0 when unknown.
    public let created: Int64

    public var id: String { name }
    /// At its maximum, reporting a health issue, or not ACTIVE (creating, updating, degraded).
    public var needsAttention: Bool { atMax || !issues.isEmpty || status != Self.active }
    /// The state the row shows: at its maximum, not ready, or fine.
    public var state: AutoscaleState { atMax ? .atMax : (needsAttention ? .notReady : .ok) }

    public init(
        name: String = "", status: String = "", desired: Int = 0, min: Int = 0, max: Int = 0,
        version: String = "", releaseVersion: String = "", amiType: String = "", capacityType: String = "",
        instanceTypes: [String] = [], diskSize: Int = 0, nodes: Int = 0, atMax: Bool = false,
        issues: [NodeGroupIssue] = [], updating: Bool = false, created: Int64 = 0
    ) {
        self.name = name
        self.status = status
        self.desired = desired
        self.min = min
        self.max = max
        self.version = version
        self.releaseVersion = releaseVersion
        self.amiType = amiType
        self.capacityType = capacityType
        self.instanceTypes = instanceTypes
        self.diskSize = diskSize
        self.nodes = nodes
        self.atMax = atMax
        self.issues = issues
        self.updating = updating
        self.created = created
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        status = try c.field(.status, "")
        desired = try c.field(.desired, 0)
        min = try c.field(.min, 0)
        max = try c.field(.max, 0)
        version = try c.field(.version, "")
        releaseVersion = try c.field(.releaseVersion, "")
        amiType = try c.field(.amiType, "")
        capacityType = try c.field(.capacityType, "")
        instanceTypes = try c.field(.instanceTypes, [])
        diskSize = try c.field(.diskSize, 0)
        nodes = try c.field(.nodes, 0)
        atMax = try c.field(.atMax, false)
        issues = try c.field(.issues, [])
        updating = try c.field(.updating, false)
        created = try c.field(.created, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case name, status, desired, min, max, version, releaseVersion, amiType, capacityType, instanceTypes
        case diskSize, nodes, atMax, issues, updating, created
    }
}

/// One of AWS' health issues on a node group, with the resources it names.
public struct NodeGroupIssue: Decodable, Equatable, Hashable, Sendable {
    public let code: String
    public let message: String
    public let resourceIds: [String]

    public init(code: String = "", message: String = "", resourceIds: [String] = []) {
        self.code = code
        self.message = message
        self.resourceIds = resourceIds
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        code = try c.field(.code, "")
        message = try c.field(.message, "")
        resourceIds = try c.field(.resourceIds, [])
    }

    private enum CodingKeys: String, CodingKey { case code, message, resourceIds }
}
