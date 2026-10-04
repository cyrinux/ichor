import Foundation

// Mirrors go/ichorgo/kube_workloads.go.

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

/// The workloads that run pods (an app's, from the inventory), through each pod's owner in
/// kubePods: a StatefulSet or DaemonSet directly, a Deployment through its ReplicaSet, named
/// `<deployment>-<pod-template-hash>`. Pods without such an owner (static, Job, bare) are left
/// out. In the order of workloads.
public func workloadOwners(_ workloads: [KubeWorkload], pods: [InventoryPod], kubePods: [KubePod]) -> [KubeWorkload] {
    let owners = Dictionary(kubePods.map { ($0.id, $0.owner) }, uniquingKeysWith: { first, _ in first })
    let wanted = Set(pods.compactMap { pod in
        owners["\(pod.namespace)/\(pod.pod)"].flatMap { workloadKey(namespace: pod.namespace, owner: $0) }
    })
    return workloads.filter { wanted.contains($0.id) }
}

/// The KubeWorkload id an owner reference ("ReplicaSet/web-5d8f") stands for; nil for other kinds.
private func workloadKey(namespace: String, owner: String) -> String? {
    guard let slash = owner.firstIndex(of: "/") else { return nil }
    let kind = owner[..<slash]
    let name = owner[owner.index(after: slash)...]
    guard !name.isEmpty else { return nil }
    switch kind {
    case "StatefulSet", "DaemonSet":
        return "\(kind)/\(namespace)/\(name)"
    case "ReplicaSet":
        guard let dash = name.lastIndex(of: "-"), dash > name.startIndex else { return nil }
        return "Deployment/\(namespace)/\(name[..<dash])"
    default:
        return nil
    }
}

/// One workload's rollout with its pods, old and new (KubeRolloutStatus), polled while it rolls out.
public struct KubeRolloutStatus: Decodable, Equatable, Sendable {
    public let workload: KubeWorkload
    /// Every pod runs the latest template and is ready, as `kubectl rollout status` ends.
    public let done: Bool
    /// The Deployment exceeded its progress deadline.
    public let failed: Bool
    /// Pods are only replaced when deleted (OnDelete, StatefulSet partition): a restart replaces none.
    public let manual: Bool
    /// The new pods first, the newest first.
    public let pods: [KubeRolloutPod]

    public init(workload: KubeWorkload, done: Bool = false, failed: Bool = false, manual: Bool = false, pods: [KubeRolloutPod] = []) {
        self.workload = workload
        self.done = done
        self.failed = failed
        self.manual = manual
        self.pods = pods
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        workload = try c.decode(KubeWorkload.self, forKey: .workload)
        done = try c.decodeIfPresent(Bool.self, forKey: .done) ?? false
        failed = try c.decodeIfPresent(Bool.self, forKey: .failed) ?? false
        manual = try c.decodeIfPresent(Bool.self, forKey: .manual) ?? false
        pods = try c.decodeIfPresent([KubeRolloutPod].self, forKey: .pods) ?? []
    }

    /// New pods running and ready.
    public var newReady: Int { pods.filter { $0.updated && $0.healthy }.count }

    private enum CodingKeys: String, CodingKey { case workload, done, failed, manual, pods }
}

/// A pod of a workload during a rollout.
public struct KubeRolloutPod: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// As `kubectl get pods`: Running, Pending, ContainerCreating, Terminating...
    public let status: String
    public let healthy: Bool
    public let ready: Int
    public let containers: Int
    public let restarts: Int
    public let node: String
    /// Unix ms.
    public let created: Int64
    /// Runs the latest pod template.
    public let updated: Bool

    public var id: String { name }

    public init(name: String, status: String = "", healthy: Bool = false, ready: Int = 0, containers: Int = 0,
                restarts: Int = 0, node: String = "", created: Int64 = 0, updated: Bool = false) {
        self.name = name
        self.status = status
        self.healthy = healthy
        self.ready = ready
        self.containers = containers
        self.restarts = restarts
        self.node = node
        self.created = created
        self.updated = updated
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        healthy = try c.decodeIfPresent(Bool.self, forKey: .healthy) ?? false
        ready = try c.decodeIfPresent(Int.self, forKey: .ready) ?? 0
        containers = try c.decodeIfPresent(Int.self, forKey: .containers) ?? 0
        restarts = try c.decodeIfPresent(Int.self, forKey: .restarts) ?? 0
        node = try c.decodeIfPresent(String.self, forKey: .node) ?? ""
        created = try c.decodeIfPresent(Int64.self, forKey: .created) ?? 0
        updated = try c.decodeIfPresent(Bool.self, forKey: .updated) ?? false
    }

    private enum CodingKeys: String, CodingKey {
        case name, status, healthy, ready, containers, restarts, node, created, updated
    }
}
