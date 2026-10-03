import Foundation

// Mirrors go/talosmobile/cgroups.go (NodeCgroups). Go omits zero fields and empty lists.

/// PSI averages: % of the last 10/60 s some (or all) tasks waited for the resource.
public struct CgroupPSI: Decodable, Equatable, Sendable {
    public let some10: Double
    public let some60: Double
    public let full10: Double
    public let full60: Double

    public init(some10: Double = 0, some60: Double = 0, full10: Double = 0, full60: Double = 0) {
        self.some10 = some10
        self.some60 = some60
        self.full10 = full10
        self.full60 = full60
    }

    private enum CodingKeys: String, CodingKey { case some10, some60, full10, full60 }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        some10 = try c.decodeIfPresent(Double.self, forKey: .some10) ?? 0
        some60 = try c.decodeIfPresent(Double.self, forKey: .some60) ?? 0
        full10 = try c.decodeIfPresent(Double.self, forKey: .full10) ?? 0
        full60 = try c.decodeIfPresent(Double.self, forKey: .full60) ?? 0
    }
}

public struct CgroupPressure: Decodable, Equatable, Sendable {
    public let cpu: CgroupPSI
    public let memory: CgroupPSI
    public let io: CgroupPSI

    public init(cpu: CgroupPSI = CgroupPSI(), memory: CgroupPSI = CgroupPSI(), io: CgroupPSI = CgroupPSI()) {
        self.cpu = cpu
        self.memory = memory
        self.io = io
    }

    /// The worst "some" 10 s average of the three, for a single figure per row.
    public var worst: Double { max(cpu.some10, memory.some10, io.some10) }
}

/// One cgroup; limits of 0 mean none, CPU (µs) and IO (bytes) are cumulative since boot.
public struct CgroupNode: Decodable, Equatable, Sendable {
    public let name: String
    public let kind: String
    public let memCurrent: UInt64
    public let memMax: UInt64
    public let oomKills: UInt64
    public let cpuUsec: UInt64
    public let ioRead: UInt64
    public let ioWrite: UInt64
    public let pressure: CgroupPressure?
    public let children: [CgroupNode]

    public init(name: String, kind: String = "group", memCurrent: UInt64 = 0, memMax: UInt64 = 0, oomKills: UInt64 = 0,
                cpuUsec: UInt64 = 0, ioRead: UInt64 = 0, ioWrite: UInt64 = 0, pressure: CgroupPressure? = nil,
                children: [CgroupNode] = []) {
        self.name = name
        self.kind = kind
        self.memCurrent = memCurrent
        self.memMax = memMax
        self.oomKills = oomKills
        self.cpuUsec = cpuUsec
        self.ioRead = ioRead
        self.ioWrite = ioWrite
        self.pressure = pressure
        self.children = children
    }

    private enum CodingKeys: String, CodingKey {
        case name, kind, memCurrent, memMax, oomKills, cpuUsec, ioRead, ioWrite, pressure, children
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "group"
        memCurrent = try c.decodeIfPresent(UInt64.self, forKey: .memCurrent) ?? 0
        memMax = try c.decodeIfPresent(UInt64.self, forKey: .memMax) ?? 0
        oomKills = try c.decodeIfPresent(UInt64.self, forKey: .oomKills) ?? 0
        cpuUsec = try c.decodeIfPresent(UInt64.self, forKey: .cpuUsec) ?? 0
        ioRead = try c.decodeIfPresent(UInt64.self, forKey: .ioRead) ?? 0
        ioWrite = try c.decodeIfPresent(UInt64.self, forKey: .ioWrite) ?? 0
        pressure = try c.decodeIfPresent(CgroupPressure.self, forKey: .pressure)
        children = try c.decodeIfPresent([CgroupNode].self, forKey: .children) ?? []
    }
}

public struct CgroupHotspot: Decodable, Equatable, Sendable {
    public let resource: String
    public let name: String
    public let parent: String

    private enum CodingKeys: String, CodingKey { case resource, name, parent }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        resource = try c.decode(String.self, forKey: .resource)
        name = try c.decode(String.self, forKey: .name)
        parent = try c.decodeIfPresent(String.self, forKey: .parent) ?? ""
    }

    /// "name (parent)": Talos has both system/runtime and podruntime/runtime.
    public var who: String { parent.isEmpty ? name : "\(name) (\(parent))" }
}

public struct CgroupAlert: Decodable, Equatable, Hashable, Sendable {
    public let kind: String
    public let name: String
    public let parent: String
    public let count: Int
    public let percent: Double

    private enum CodingKeys: String, CodingKey { case kind, name, parent, count, percent }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decode(String.self, forKey: .kind)
        name = try c.decode(String.self, forKey: .name)
        parent = try c.decodeIfPresent(String.self, forKey: .parent) ?? ""
        count = try c.decodeIfPresent(Int.self, forKey: .count) ?? 0
        percent = try c.decodeIfPresent(Double.self, forKey: .percent) ?? 0
    }

    /// The workload, with its pod or group when it has one: "web (default/web-0)".
    public var who: String { parent.isEmpty ? name : "\(name) (\(parent))" }
}

public struct CgroupReport: Decodable, Equatable, Sendable {
    /// Unix milliseconds, to turn CPU and IO deltas into rates.
    public let at: Int64
    public let pressure: CgroupPressure
    public let hotspots: [CgroupHotspot]
    public let alerts: [CgroupAlert]
    public let root: CgroupNode?

    public init(at: Int64, pressure: CgroupPressure = CgroupPressure(), root: CgroupNode?) {
        self.at = at
        self.pressure = pressure
        self.hotspots = []
        self.alerts = []
        self.root = root
    }

    private enum CodingKeys: String, CodingKey { case at, pressure, hotspots, alerts, root }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.decode(Int64.self, forKey: .at)
        pressure = try c.decodeIfPresent(CgroupPressure.self, forKey: .pressure) ?? CgroupPressure()
        hotspots = try c.decodeIfPresent([CgroupHotspot].self, forKey: .hotspots) ?? []
        alerts = try c.decodeIfPresent([CgroupAlert].self, forKey: .alerts) ?? []
        root = try c.decodeIfPresent(CgroupNode.self, forKey: .root)
    }

    /// The workload that waits the most on resource ("cpu", "memory", "io"), if any.
    public func mostAffected(_ resource: String) -> String? {
        hotspots.first { $0.resource == resource }?.who
    }
}

public enum PressureLevel: Sendable {
    case ok, warn, bad
}

/// Below 5% tasks barely wait; from 20% the node is visibly slowed (the usual PSI alert thresholds).
public func pressureLevel(_ some10: Double) -> PressureLevel {
    if some10 >= 20 { return .bad }
    return some10 >= 5 ? .warn : .ok
}

/// A cgroup in the flattened tree, with its CPU and IO rates since the previous sample.
public struct CgroupRow: Equatable, Identifiable, Sendable {
    public let id: String
    public let depth: Int
    public let node: CgroupNode
    /// Percent of one core (like top); nil without a usable previous sample.
    public let cpuPercent: Double?
    /// Bytes read + written per second; nil without a usable previous sample.
    public let ioPerSecond: Double?

    public var hasChildren: Bool { !node.children.isEmpty }
}

public enum CgroupSort: String, CaseIterable, Sendable {
    case memory, cpu, pressure
}

/// The tree as rows, depth first, below the root (the whole node, which the pressure section
/// already shows): children of `expanded` paths only, siblings sorted by `sort`.
public func cgroupRows(previous: CgroupReport?, current: CgroupReport, expanded: Set<String>, sort: CgroupSort) -> [CgroupRow] {
    guard let root = current.root else { return [] }
    let seconds = previous.map { Double(current.at - $0.at) / 1000 } ?? 0
    let before = previous?.root.map(cgroupsByPath) ?? [:]
    var rows: [CgroupRow] = []

    func rate(_ now: UInt64, _ old: UInt64?) -> Double? {
        guard seconds > 0, let old, now >= old else { return nil }
        return Double(now - old) / seconds
    }

    func add(_ node: CgroupNode, path: String, depth: Int) {
        let old = before[path]
        let cpu = rate(node.cpuUsec, old?.cpuUsec).map { $0 / 1e6 * 100 }
        let io = rate(node.ioRead &+ node.ioWrite, old.map { $0.ioRead &+ $0.ioWrite })
        rows.append(CgroupRow(id: path, depth: depth, node: node, cpuPercent: cpu, ioPerSecond: io))
        guard expanded.contains(path) else { return }
        for (child, childPath) in sortedChildren(node, path: path, sort: sort, before: before) {
            add(child, path: childPath, depth: depth + 1)
        }
    }

    for (child, path) in sortedChildren(root, path: "", sort: sort, before: before) {
        add(child, path: path, depth: 0)
    }
    return rows
}

private func sortedChildren(_ node: CgroupNode, path: String, sort: CgroupSort,
                            before: [String: CgroupNode]) -> [(CgroupNode, String)] {
    let children = node.children.map { ($0, childPath(path, $0.name)) }
    return children.sorted { a, b in
        switch sort {
        case .memory: return a.0.memCurrent > b.0.memCurrent
        case .pressure: return (a.0.pressure?.worst ?? 0) > (b.0.pressure?.worst ?? 0)
        // CPU since the previous sample; without one, nothing to compare (not the since-boot total).
        case .cpu:
            func delta(_ node: CgroupNode, _ path: String) -> UInt64 {
                guard let old = before[path]?.cpuUsec, node.cpuUsec >= old else { return 0 }
                return node.cpuUsec - old
            }
            return delta(a.0, a.1) > delta(b.0, b.1)
        }
    }
}

private func childPath(_ parent: String, _ name: String) -> String {
    parent.isEmpty ? name : parent + "\u{0}" + name
}

public func cgroupsByPath(_ root: CgroupNode) -> [String: CgroupNode] {
    var out: [String: CgroupNode] = [:]
    func walk(_ node: CgroupNode, _ path: String) {
        for child in node.children {
            let p = childPath(path, child.name)
            out[p] = child
            walk(child, p)
        }
    }
    walk(root, "")
    return out
}

/// Paths open on first show: the Talos groups (system, podruntime), so their services are
/// visible. kubepods stays closed: its pods are the Pods tab's, open it to compare them.
public func defaultExpandedCgroups(_ report: CgroupReport) -> Set<String> {
    Set((report.root?.children ?? []).filter { !$0.children.isEmpty && $0.name != "kubepods" }.map(\.name))
}
