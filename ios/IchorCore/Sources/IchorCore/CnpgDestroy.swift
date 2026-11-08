import Foundation

/// What removing one instance of a CloudNativePG cluster would do, like `kubectl cnpg destroy`.
public struct CnpgDestroyPreview: Decodable, Equatable, Sendable {
    public let namespace, name, instance, currentPrimary, targetPrimary: String
    /// "" when the instance has no pod.
    public let podPhase, podNode: String
    public let volumes: [CnpgDestroyVolume]
    /// primary, noOtherData or notFound: the removal is refused.
    public let blockers: [String]
    /// ready or nodePresent.
    public let warnings: [String]
    public let canDestroy: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        instance = try c.field(.instance, "")
        currentPrimary = try c.field(.currentPrimary, "")
        targetPrimary = try c.field(.targetPrimary, "")
        podPhase = try c.field(.podPhase, "")
        podNode = try c.field(.podNode, "")
        volumes = try c.field(.volumes, [])
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
        canDestroy = try c.field(.canDestroy, false)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, instance, currentPrimary, targetPrimary, podPhase, podNode, volumes, blockers, warnings
        case canDestroy
    }
}

public struct CnpgDestroyVolume: Decodable, Equatable, Sendable {
    public let name: String
    /// PG_DATA, PG_WAL, PG_TABLESPACE, "" on older clusters.
    public let role: String
    public let node: String
    /// present, gone, unknown or none.
    public let nodeState: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        role = try c.field(.role, "")
        node = try c.field(.node, "")
        nodeState = try c.field(.nodeState, "")
    }

    private enum CodingKeys: String, CodingKey { case name, role, node, nodeState }
}

public extension CnpgCluster {
    /// Instances worth removing: failed, without a pod, or waiting unscheduled; never the current or
    /// target primary. The Go core checks again before deleting anything.
    var removableInstances: [String] {
        let pending = instancePods.filter { $0.node.isEmpty && $0.phase == "Pending" }.map(\.name)
        let all = Set(failedInstances + missingInstances + pending)
        return all.filter { !$0.isEmpty && $0 != currentPrimary && $0 != targetPrimary }.sorted()
    }
}
