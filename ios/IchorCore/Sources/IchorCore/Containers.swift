import Foundation

/// Containerd namespaces of a node's containers: Talos' own (apid, trustd, extension services),
/// the ones a ContainerConfig declares (Talos 1.15, run without Kubernetes) and the Kubernetes ones.
public enum ContainerNamespace {
    public static let system = "system"
    public static let talos = "taloscontainers"
    public static let kubernetes = "k8s.io"
}

/// A Talos container's health as a tone: healthy, degraded or on its way (pending, pulling).
public enum TalosContainerHealth: Equatable, Sendable {
    case healthy, degraded, starting
}

/// One sample of a node's system and Kubernetes containers from the Go core (NodeContainers).
public struct ContainerSample: Decodable, Equatable, Sendable {
    /// Unix milliseconds, to turn CPU nanosecond deltas into a percentage.
    public let at: Int64
    public let containers: [NodeContainer]

    public init(at: Int64, containers: [NodeContainer]) {
        self.at = at
        self.containers = containers
    }

    private enum CodingKeys: String, CodingKey { case at, containers }

    // Go encodes an empty (nil) slice as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.decode(Int64.self, forKey: .at)
        containers = try c.field(.containers, [])
    }
}

public struct NodeContainer: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    /// Containerd namespace: `ContainerNamespace.system`, `.talos` or `.kubernetes`.
    public let namespace: String
    public let podNamespace: String
    public let pod: String
    public let name: String
    public let image: String
    public let status: String
    public let pid: UInt32
    /// Bytes.
    public let memory: UInt64
    /// Cumulative CPU time, nanoseconds.
    public let cpuNanos: UInt64
    // A Talos container's ContainerStatus (Talos 1.15); empty for the other namespaces.
    /// pending, pulling, healthy or degraded.
    public let health: String
    public let restartCount: UInt64
    public let exitCode: Int32
    public let error: String
    /// The unmet gates (image, mounts, dependencies) while pending.
    public let waitingFor: [String]
    /// What it wrote before it failed: workload output, shown as text, never notified.
    public let lastLog: String

    public init(
        id: String, namespace: String = ContainerNamespace.kubernetes, podNamespace: String = "default", pod: String,
        name: String, image: String = "", status: String = "CONTAINER_RUNNING", pid: UInt32 = 0, memory: UInt64 = 0,
        cpuNanos: UInt64 = 0, health: String = "", restartCount: UInt64 = 0, exitCode: Int32 = 0, error: String = "",
        waitingFor: [String] = [], lastLog: String = ""
    ) {
        self.id = id
        self.namespace = namespace
        self.podNamespace = podNamespace
        self.pod = pod
        self.name = name
        self.image = image
        self.status = status
        self.pid = pid
        self.memory = memory
        self.cpuNanos = cpuNanos
        self.health = health
        self.restartCount = restartCount
        self.exitCode = exitCode
        self.error = error
        self.waitingFor = waitingFor
        self.lastLog = lastLog
    }

    private enum CodingKeys: String, CodingKey {
        case id, namespace, podNamespace, pod, name, image, status, pid, memory, cpuNanos
        case health, restartCount, exitCode, error, waitingFor, lastLog
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        namespace = try c.field(.namespace, ContainerNamespace.kubernetes)
        podNamespace = try c.field(.podNamespace, "")
        pod = try c.field(.pod, "")
        name = try c.field(.name, "")
        image = try c.field(.image, "")
        status = try c.field(.status, "")
        pid = try c.field(.pid, 0)
        memory = try c.field(.memory, 0)
        cpuNanos = try c.field(.cpuNanos, 0)
        health = try c.field(.health, "")
        restartCount = try c.field(.restartCount, 0)
        exitCode = try c.field(.exitCode, 0)
        error = try c.field(.error, "")
        waitingFor = try c.field(.waitingFor, [])
        lastLog = try c.field(.lastLog, "")
    }

    /// A Talos container (apid, trustd, an extension service), not a Kubernetes one.
    public var isSystem: Bool { namespace == ContainerNamespace.system }

    /// A container a ContainerConfig declares.
    public var isTalosContainer: Bool { namespace == ContainerNamespace.talos }

    /// A Talos container's health tone; failed (given up) is degraded whatever it last reported.
    public var talosHealth: TalosContainerHealth {
        if status.lowercased() == "failed" || health.lowercased() == "degraded" { return .degraded }
        return health.lowercased() == "healthy" ? .healthy : .starting
    }

    /// The line a row shows for a Talos container in trouble: the last line it logged, else its error.
    public var lastErrorLine: String {
        let lines = lastLog.split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }
        return lines.last { !$0.isEmpty } ?? error.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The name shown in lists and dialogs.
    public var displayName: String { name.isEmpty ? String(id.prefix(12)) : name }

    /// "CONTAINER_RUNNING" (CRI) or "RUNNING".
    public var isRunning: Bool { status.uppercased().hasSuffix("RUNNING") }

    /// "running", "exited"… without the CRI prefix.
    public var displayStatus: String {
        let upper = status.uppercased()
        let bare = upper.hasPrefix("CONTAINER_") ? String(status.dropFirst("CONTAINER_".count)) : status
        return bare.lowercased()
    }
}

/// A container with its CPU usage since the previous sample (nil until there is one).
public struct ContainerRow: Equatable, Identifiable, Sendable {
    public let container: NodeContainer
    /// Percent of one CPU.
    public let cpuPercent: Double?

    public var id: String { container.id }

    public init(container: NodeContainer, cpuPercent: Double?) {
        self.container = container
        self.cpuPercent = cpuPercent
    }
}

/// CPU% per container id from the CPU time delta over the wall-clock delta (×100). A new id has
/// no value yet; a counter that went backwards (container restarted) gives 0.
public func containerRows(previous: ContainerSample?, current: ContainerSample) -> [ContainerRow] {
    var before: [String: NodeContainer] = [:]
    var seconds = 0.0
    if let previous {
        seconds = Double(current.at - previous.at) / 1000
        for container in previous.containers { before[container.id] = container }
    }
    return current.containers.map { container in
        guard seconds > 0, let old = before[container.id] else {
            return ContainerRow(container: container, cpuPercent: nil)
        }
        let delta = container.cpuNanos >= old.cpuNanos ? Double(container.cpuNanos - old.cpuNanos) : 0
        return ContainerRow(container: container, cpuPercent: delta / 1e9 / seconds * 100)
    }
}

/// A pod's containers, or Talos' own containers when `isSystem`, or the containers a
/// ContainerConfig declares when `isTalos`, with totals.
public struct PodGroup: Equatable, Identifiable, Sendable {
    public let namespace: String
    public let pod: String
    public let containers: [ContainerRow]
    public let isSystem: Bool
    public let isTalos: Bool

    public var id: String {
        if isSystem { return ContainerNamespace.system }
        return isTalos ? ContainerNamespace.talos : "\(namespace)/\(pod)"
    }

    /// Where the group lists: Talos' own containers, then the Talos containers, then the pods.
    var rank: Int { isSystem ? 0 : isTalos ? 1 : 2 }

    public init(
        namespace: String, pod: String, containers: [ContainerRow], isSystem: Bool = false, isTalos: Bool = false
    ) {
        self.namespace = namespace
        self.pod = pod
        self.containers = containers
        self.isSystem = isSystem
        self.isTalos = isTalos
    }

    public var memory: UInt64 { containers.reduce(UInt64(0)) { $0 &+ $1.container.memory } }

    /// Sum of the known values; nil while no container has one.
    public var cpuPercent: Double? {
        let known = containers.compactMap(\.cpuPercent)
        return known.isEmpty ? nil : known.reduce(0, +)
    }

    public var allRunning: Bool { containers.allSatisfy(\.container.isRunning) }
}

/// Groups rows by namespace/pod (Talos' system containers in one group, the Talos containers in
/// another), keeping the first-seen order of pods and containers.
public func groupPods(_ rows: [ContainerRow]) -> [PodGroup] {
    var order: [String] = []
    var byPod: [String: [ContainerRow]] = [:]
    var names: [String: (String, String)] = [:]
    for row in rows {
        let key =
            row.container.isSystem || row.container.isTalosContainer
            ? row.container.namespace : "\(row.container.podNamespace)/\(row.container.pod)"
        if byPod[key] == nil {
            order.append(key)
            names[key] = (row.container.podNamespace, row.container.pod)
        }
        byPod[key, default: []].append(row)
    }
    return order.compactMap { key in
        guard let (namespace, pod) = names[key], let containers = byPod[key] else { return nil }
        let system = containers.first?.container.isSystem ?? false
        let talos = containers.first?.container.isTalosContainer ?? false
        let grouped = system || talos
        return PodGroup(
            namespace: grouped ? "" : namespace, pod: grouped ? "" : pod, containers: containers, isSystem: system,
            isTalos: talos)
    }
}

/// The system group first, then the Talos containers, then highest CPU (unknown last) or highest
/// memory first; ties by memory then name.
public func sortPods(_ pods: [PodGroup], by sort: ProcessSort) -> [PodGroup] {
    pods.sorted { a, b in
        if a.rank != b.rank { return a.rank < b.rank }
        if sort == .cpu {
            let ca = a.cpuPercent ?? -1
            let cb = b.cpuPercent ?? -1
            if ca != cb { return ca > cb }
        }
        if a.memory != b.memory { return a.memory > b.memory }
        return a.id < b.id
    }
}

/// Case-insensitive match on namespace, pod, container name or image; a pod matched by its own
/// name keeps all its containers. An empty query keeps everything.
public func filterPods(_ pods: [PodGroup], query: String) -> [PodGroup] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    guard !needle.isEmpty else { return pods }
    func has(_ text: String) -> Bool { text.range(of: needle, options: .caseInsensitive) != nil }
    return pods.compactMap { pod in
        if has(pod.namespace) || has(pod.pod) || has(pod.id) { return pod }
        let matching = pod.containers.filter { has($0.container.name) || has($0.container.image) }
        return matching.isEmpty
            ? nil
            : PodGroup(
                namespace: pod.namespace, pod: pod.pod, containers: matching, isSystem: pod.isSystem,
                isTalos: pod.isTalos)
    }
}
