import Foundation

/// One sample of a node's Kubernetes containers from the Go core (NodeContainers).
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
        containers = try c.decodeIfPresent([NodeContainer].self, forKey: .containers) ?? []
    }
}

public struct NodeContainer: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
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

    public init(id: String, podNamespace: String = "default", pod: String, name: String, image: String = "",
                status: String = "CONTAINER_RUNNING", pid: UInt32 = 0, memory: UInt64 = 0, cpuNanos: UInt64 = 0) {
        self.id = id
        self.podNamespace = podNamespace
        self.pod = pod
        self.name = name
        self.image = image
        self.status = status
        self.pid = pid
        self.memory = memory
        self.cpuNanos = cpuNanos
    }

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

/// A pod's containers with totals.
public struct PodGroup: Equatable, Identifiable, Sendable {
    public let namespace: String
    public let pod: String
    public let containers: [ContainerRow]

    public var id: String { "\(namespace)/\(pod)" }

    public init(namespace: String, pod: String, containers: [ContainerRow]) {
        self.namespace = namespace
        self.pod = pod
        self.containers = containers
    }

    public var memory: UInt64 { containers.reduce(UInt64(0)) { $0 &+ $1.container.memory } }

    /// Sum of the known values; nil while no container has one.
    public var cpuPercent: Double? {
        let known = containers.compactMap(\.cpuPercent)
        return known.isEmpty ? nil : known.reduce(0, +)
    }

    public var allRunning: Bool { containers.allSatisfy(\.container.isRunning) }
}

/// Groups rows by namespace/pod, keeping the first-seen order of pods and containers.
public func groupPods(_ rows: [ContainerRow]) -> [PodGroup] {
    var order: [String] = []
    var byPod: [String: [ContainerRow]] = [:]
    var names: [String: (String, String)] = [:]
    for row in rows {
        let key = "\(row.container.podNamespace)/\(row.container.pod)"
        if byPod[key] == nil {
            order.append(key)
            names[key] = (row.container.podNamespace, row.container.pod)
        }
        byPod[key, default: []].append(row)
    }
    return order.compactMap { key in
        guard let (namespace, pod) = names[key], let containers = byPod[key] else { return nil }
        return PodGroup(namespace: namespace, pod: pod, containers: containers)
    }
}

/// Highest CPU (unknown last) or highest memory first; ties by memory then name.
public func sortPods(_ pods: [PodGroup], by sort: ProcessSort) -> [PodGroup] {
    pods.sorted { a, b in
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
        return matching.isEmpty ? nil : PodGroup(namespace: pod.namespace, pod: pod.pod, containers: matching)
    }
}
