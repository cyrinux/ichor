import Foundation

// Mirrors go/talosmobile/kube_workloads.go.

public struct KubeWorkloadList: Decodable, Equatable, Sendable {
    public let workloads: [KubeWorkload]

    public init(workloads: [KubeWorkload] = []) { self.workloads = workloads }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        workloads = try c.decodeIfPresent([KubeWorkload].self, forKey: .workloads) ?? []
    }

    private enum CodingKeys: String, CodingKey { case workloads }
}

/// A Deployment, StatefulSet or DaemonSet with its rollout state (KubeWorkloads).
public struct KubeWorkload: Decodable, Equatable, Identifiable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let desired: Int
    public let ready: Int
    public let updated: Int
    public let available: Int
    /// ready, progressing, degraded, paused or scaledDown.
    public let state: String
    /// Unix ms of the last rollout restart, 0 when never.
    public let restartedAt: Int64
    /// Unix ms.
    public let created: Int64
    public let images: [String]

    public var id: String { "\(kind)/\(namespace)/\(name)" }

    public var workloadState: WorkloadState { WorkloadState(rawValue: state) ?? .unknown }

    /// kubectl refuses to restart a paused Deployment, and so does the Go core.
    public var canRestart: Bool { workloadState != .paused }

    public init(kind: String, namespace: String, name: String, desired: Int = 0, ready: Int = 0, updated: Int = 0,
                available: Int = 0, state: String = "", restartedAt: Int64 = 0, created: Int64 = 0, images: [String] = []) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.desired = desired
        self.ready = ready
        self.updated = updated
        self.available = available
        self.state = state
        self.restartedAt = restartedAt
        self.created = created
        self.images = images
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decode(String.self, forKey: .kind)
        namespace = try c.decode(String.self, forKey: .namespace)
        name = try c.decode(String.self, forKey: .name)
        desired = try c.decodeIfPresent(Int.self, forKey: .desired) ?? 0
        ready = try c.decodeIfPresent(Int.self, forKey: .ready) ?? 0
        updated = try c.decodeIfPresent(Int.self, forKey: .updated) ?? 0
        available = try c.decodeIfPresent(Int.self, forKey: .available) ?? 0
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? ""
        restartedAt = try c.decodeIfPresent(Int64.self, forKey: .restartedAt) ?? 0
        created = try c.decodeIfPresent(Int64.self, forKey: .created) ?? 0
        images = try c.decodeIfPresent([String].self, forKey: .images) ?? []
    }

    private enum CodingKeys: String, CodingKey {
        case kind, namespace, name, desired, ready, updated, available, state, restartedAt, created, images
    }
}

public enum WorkloadState: String, Sendable {
    case ready, progressing, degraded, paused, scaledDown, unknown

    /// Degraded first, then rolling out, then the rest.
    var attentionRank: Int {
        switch self {
        case .degraded: 0
        case .progressing: 1
        default: 2
        }
    }
}

/// Namespaces that have workloads, sorted.
public func workloadNamespaces(_ workloads: [KubeWorkload]) -> [String] {
    Array(Set(workloads.map(\.namespace))).sorted()
}

/// Workloads of namespace (all when nil) whose name, kind or image contains query
/// (case-insensitive), the ones that need attention (degraded, progressing) first.
public func filterWorkloads(_ workloads: [KubeWorkload], namespace: String?, query: String) -> [KubeWorkload] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    let contains = { (text: String) in text.range(of: needle, options: .caseInsensitive) != nil }
    return workloads
        .filter { w in
            (namespace == nil || w.namespace == namespace) &&
                (needle.isEmpty || contains(w.name) || contains(w.kind) || w.images.contains(where: contains))
        }
        .sorted { a, b in
            let ra = a.workloadState.attentionRank, rb = b.workloadState.attentionRank
            if ra != rb { return ra < rb }
            if a.namespace != b.namespace { return a.namespace < b.namespace }
            if a.name != b.name { return a.name < b.name }
            return a.kind < b.kind
        }
}
