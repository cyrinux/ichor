import Foundation

/// `kubectl top` from metrics-server (Go `KubeTopNodes`/`KubeTopPods`). CPU in cores, memory in
/// bytes. `available` false: no metrics-server; `forbidden`: the credentials may not read it.
public struct KubeTopNodes: Decodable, Sendable {
    public let available: Bool
    public let forbidden: Bool
    public let nodes: [KubeTopNode]

    public var byName: [String: KubeTopNode] { Dictionary(nodes.map { ($0.name, $0) }, uniquingKeysWith: { a, _ in a }) }

    private enum CodingKeys: String, CodingKey { case available, forbidden, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        available = try c.field(.available, false)
        forbidden = try c.field(.forbidden, false)
        nodes = try c.field(.nodes, [])
    }
}

public struct KubeTopNode: Decodable, Hashable, Sendable {
    public let name: String
    public let cpu: Double
    public let memory: Double
    public let cpuAllocatable: Double
    public let memoryAllocatable: Double
    public let cpuPercent: Double
    public let memoryPercent: Double

    private enum CodingKeys: String, CodingKey {
        case name, cpu, memory, cpuAllocatable, memoryAllocatable, cpuPercent, memoryPercent
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        cpu = try c.field(.cpu, 0)
        memory = try c.field(.memory, 0)
        cpuAllocatable = try c.field(.cpuAllocatable, 0)
        memoryAllocatable = try c.field(.memoryAllocatable, 0)
        cpuPercent = try c.field(.cpuPercent, 0)
        memoryPercent = try c.field(.memoryPercent, 0)
    }
}

public struct KubeTopPods: Decodable, Sendable {
    public let available: Bool
    public let forbidden: Bool
    /// The requests and limits were read: one namespace or one pod. False for every namespace (usage only).
    public let boundsRead: Bool
    public let pods: [KubeTopPod]

    /// By `KubeTopPod.id` ("namespace/name"), the id pod rows use.
    public var byKey: [String: KubeTopPod] { Dictionary(pods.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a }) }

    private enum CodingKeys: String, CodingKey { case available, forbidden, boundsRead, pods }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        available = try c.field(.available, false)
        forbidden = try c.field(.forbidden, false)
        boundsRead = try c.field(.boundsRead, false)
        pods = try c.field(.pods, [])
    }
}

/// A pod's usage with its requests and limits; a limit of 0 means some container has none.
public struct KubeTopPod: Decodable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let node: String
    public let cpu: Double
    public let memory: Double
    public let cpuRequest: Double
    public let cpuLimit: Double
    public let memoryRequest: Double
    public let memoryLimit: Double

    public var id: String { "\(namespace)/\(name)" }

    /// CPU use as a share of the limit, else of the request; nil when neither is set.
    public var cpuFraction: Double? { fraction(cpu, of: cpuLimit > 0 ? cpuLimit : cpuRequest) }
    public var memoryFraction: Double? { fraction(memory, of: memoryLimit > 0 ? memoryLimit : memoryRequest) }
    /// What the bar measures against: the limit, else the request, 0 for neither.
    public var cpuBound: Double { cpuLimit > 0 ? cpuLimit : cpuRequest }
    public var memoryBound: Double { memoryLimit > 0 ? memoryLimit : memoryRequest }

    public init(namespace: String, name: String, node: String = "", cpu: Double = 0, memory: Double = 0,
                cpuRequest: Double = 0, cpuLimit: Double = 0, memoryRequest: Double = 0, memoryLimit: Double = 0) {
        self.namespace = namespace
        self.name = name
        self.node = node
        self.cpu = cpu
        self.memory = memory
        self.cpuRequest = cpuRequest
        self.cpuLimit = cpuLimit
        self.memoryRequest = memoryRequest
        self.memoryLimit = memoryLimit
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, node, cpu, memory, cpuRequest, cpuLimit, memoryRequest, memoryLimit
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        node = try c.field(.node, "")
        cpu = try c.field(.cpu, 0)
        memory = try c.field(.memory, 0)
        cpuRequest = try c.field(.cpuRequest, 0)
        cpuLimit = try c.field(.cpuLimit, 0)
        memoryRequest = try c.field(.memoryRequest, 0)
        memoryLimit = try c.field(.memoryLimit, 0)
    }
}

private func fraction(_ used: Double, of total: Double) -> Double? {
    total > 0 ? min(max(used / total, 0), 1) : nil
}

/// CPU as kubectl prints it: millicores under one core ("125m"), cores above ("1.5").
public func formatCPU(_ cores: Double) -> String {
    if cores < 0.9995 { return "\(Int((cores * 1000).rounded()))m" }
    if cores.truncatingRemainder(dividingBy: 1) == 0 { return String(Int(cores)) }
    let text = String(format: "%.1f", locale: Locale(identifier: "en_US_POSIX"), cores)
    return text.hasSuffix(".0") ? String(text.dropLast(2)) : text
}

/// How a usage list is ordered.
public enum TopSort: String, CaseIterable, Sendable {
    case name, cpu, memory
}

public extension Array {
    /// Busiest first for cpu and memory, rows without metrics last; `.name` keeps the order.
    func sortedByUsage(_ sort: TopSort, usage: (Element) -> KubeTopPod?) -> [Element] {
        switch sort {
        case .name: return self
        case .cpu: return sortedStably { (usage($0)?.cpu ?? -1) > (usage($1)?.cpu ?? -1) }
        case .memory: return sortedStably { (usage($0)?.memory ?? -1) > (usage($1)?.memory ?? -1) }
        }
    }

    private func sortedStably(by before: (Element, Element) -> Bool) -> [Element] {
        enumerated().sorted { a, b in
            if before(a.element, b.element) { return true }
            if before(b.element, a.element) { return false }
            return a.offset < b.offset
        }.map(\.element)
    }
}
