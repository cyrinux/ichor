import Foundation

// Mirrors go/ichorgo/kube_nodegroups_eks_scale.go: a group's size changed from the phone
// (UpdateNodegroupConfig) and the update AWS runs for it (DescribeUpdate).

/// The update AWS runs on a group: InProgress, Failed, Cancelled or Successful, and why it failed.
public struct NodeGroupUpdate: Decodable, Equatable, Hashable, Sendable {
    public static let inProgressStatus = "InProgress"
    public static let successfulStatus = "Successful"
    /// The app's own status of an update it could not read any more (the error says why).
    public static let unreadableStatus = "Unreadable"

    public let id: String
    public let status: String
    public let type: String
    public let errors: [NodeGroupIssue]

    public var inProgress: Bool { status == Self.inProgressStatus }
    public var successful: Bool { status == Self.successfulStatus }
    public var unreadable: Bool { status == Self.unreadableStatus }

    /// Why it failed, was cancelled or could not be read: the first error's message, else the status.
    public var failure: String? {
        if inProgress || successful { return nil }
        let message = errors.first?.message ?? ""
        return message.isEmpty ? status : message
    }

    public init(id: String = "", status: String = "", type: String = "", errors: [NodeGroupIssue] = []) {
        self.id = id
        self.status = status
        self.type = type
        self.errors = errors
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        status = try c.field(.status, "")
        type = try c.field(.type, "")
        errors = try c.field(.errors, [])
    }

    /// This update, ended because it could not be read: `message` says why.
    public func unreadable(_ message: String) -> NodeGroupUpdate {
        NodeGroupUpdate(id: id, status: Self.unreadableStatus, type: type, errors: [NodeGroupIssue(message: message)])
    }

    private enum CodingKeys: String, CodingKey { case id, status, type, errors }
}

/// Why a desired / minimum / maximum size is refused before any call (Go: validateNodeGroupSize).
public enum NodeGroupSizeProblem: Equatable, Sendable {
    case minNegative, maxTooLow, minAboveMax, desiredOutside
}

/// The largest size the size sheet offers (EKS' own limit per group is set by quota).
public let nodeGroupMaxSize = 450

public func nodeGroupSizeProblem(desired: Int, min: Int, max: Int) -> NodeGroupSizeProblem? {
    if min < 0 { return .minNegative }
    if max < 1 { return .maxTooLow }
    if min > max { return .minAboveMax }
    if desired < min || desired > max { return .desiredOutside }
    return nil
}

extension ManagedNodeGroupRow {
    /// EKS' status of a group whose instances do not all launch: its size may still change.
    public static let degraded = "DEGRADED"

    /// Active or degraded: a group already updating (or being created or deleted) may not change its size.
    public var canChangeSize: Bool { !updating && (status == Self.active || status == Self.degraded) }
}
