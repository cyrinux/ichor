import Foundation

// Mirrors go/ichorgo/kube_capacity.go and kube_capacity_nodeloss.go: the cluster's free room,
// each workload's priority, QoS and OOM risk, and what losing a node would cost. CPU in cores,
// memory in bytes.

/// A pod's QoS class: who the kubelet evicts first under node pressure (BestEffort).
public enum QosClass: String, Sendable, WireEnum {
    case bestEffort = "BestEffort", burstable = "Burstable", guaranteed = "Guaranteed", unknown = ""

    public static let wireFallback: Self = .unknown
}

/// A workload's risk word, worst first; a word a newer core adds reads as `ok`.
public enum CapacityRisk: String, Sendable, WireEnum {
    case oom, evictFirst, unbounded, ok

    public static let wireFallback: Self = .ok
}

/// Where the system classes start: system-cluster-critical is 2000000000.
public let systemPriority = 1_000_000_000

/// A system class, by name or by value: never preempted, never suggested.
public func isSystemClass(_ name: String, priority: Int) -> Bool {
    name.hasPrefix("system-") || priority >= systemPriority
}

/// `part` as a share of `of`, 0...1; 0 when `of` is 0.
public func freeFraction(_ part: Double, of: Double) -> Double {
    of > 0 ? min(max(part / of, 0), 1) : 0
}

/// The cluster's capacity (Go `KubeCapacity`). `metricsAvailable` false: no metrics-server.
public struct KubeCapacity: Decodable, Equatable, Sendable {
    public let metricsAvailable: Bool
    public let metricsForbidden: Bool
    public let priorityClassesRead: Bool
    public let pdbsRead: Bool
    public let totals: CapacityTotals
    public let nodes: [CapacityNode]
    /// What goes first first: lowest priority, no class before a class of the same value.
    public let workloads: [CapacityWorkload]
    public let nPlusOne: NPlusOne
    public let assumptions: [String]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        metricsAvailable = try c.field(.metricsAvailable, false)
        metricsForbidden = try c.field(.metricsForbidden, false)
        priorityClassesRead = try c.field(.priorityClassesRead, false)
        pdbsRead = try c.field(.pdbsRead, false)
        totals = try c.field(.totals, CapacityTotals())
        nodes = try c.field(.nodes, [])
        workloads = try c.field(.workloads, [])
        nPlusOne = try c.field(.nPlusOne, NPlusOne())
        assumptions = try c.field(.assumptions, [])
    }

    private enum CodingKeys: String, CodingKey {
        case metricsAvailable, metricsForbidden, priorityClassesRead, pdbsRead, totals, nodes, workloads, nPlusOne, assumptions
    }
}

public struct CapacityTotals: Decodable, Equatable, Sendable {
    public var cpuAllocatable = 0.0
    public var cpuRequests = 0.0
    public var memoryAllocatable = 0.0
    public var memoryRequests = 0.0
    public var pods = 0
    public var podCapacity = 0
    public var nodes = 0
    public var schedulableNodes = 0

    public var cpuFree: Double { max(cpuAllocatable - cpuRequests, 0) }
    public var memoryFree: Double { max(memoryAllocatable - memoryRequests, 0) }
    public var podsFree: Int { max(podCapacity - pods, 0) }

    public init() {}

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cpuAllocatable = try c.field(.cpuAllocatable, 0)
        cpuRequests = try c.field(.cpuRequests, 0)
        memoryAllocatable = try c.field(.memoryAllocatable, 0)
        memoryRequests = try c.field(.memoryRequests, 0)
        pods = try c.field(.pods, 0)
        podCapacity = try c.field(.podCapacity, 0)
        nodes = try c.field(.nodes, 0)
        schedulableNodes = try c.field(.schedulableNodes, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case cpuAllocatable, cpuRequests, memoryAllocatable, memoryRequests, pods, podCapacity, nodes, schedulableNodes
    }
}

/// A node's requests and usage against what it offers; `overcommitted`: its pods' memory limits exceed it.
public struct CapacityNode: Decodable, Equatable, Sendable, Identifiable {
    public let name: String
    public let roles: [String]
    public let ready: Bool
    public let cordoned: Bool
    public let taints: [String]
    public let memoryPercent: Double
    public let memoryAllocatable: Double
    public let memoryUsage: Double
    public let cpuFree: Double
    public let memoryFree: Double
    public let podsFree: Int
    public let overcommitted: Bool

    public var id: String { name }

    /// Ordinary pods may land on it: ready, not cordoned, no NoSchedule/NoExecute taint.
    public var schedulable: Bool {
        ready && !cordoned && !taints.contains { $0.hasSuffix(":NoSchedule") || $0.hasSuffix(":NoExecute") }
    }

    /// Memory in use against allocatable, 0...1 (0 without metrics-server).
    public var memoryUsageFraction: Double { freeFraction(memoryUsage, of: memoryAllocatable) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        roles = try c.field(.roles, [])
        ready = try c.field(.ready, false)
        cordoned = try c.field(.cordoned, false)
        taints = try c.field(.taints, [])
        memoryPercent = try c.field(.memoryPercent, 0)
        memoryAllocatable = try c.field(.memoryAllocatable, 0)
        memoryUsage = try c.field(.memoryUsage, 0)
        cpuFree = try c.field(.cpuFree, 0)
        memoryFree = try c.field(.memoryFree, 0)
        podsFree = try c.field(.podsFree, 0)
        overcommitted = try c.field(.overcommitted, false)
    }

    private enum CodingKeys: String, CodingKey {
        case name, roles, ready, cordoned, taints, memoryPercent, memoryAllocatable, memoryUsage, cpuFree, memoryFree, podsFree, overcommitted
    }
}

/// The running pods of one controller: one replica's requests, its class, QoS and risk.
public struct CapacityWorkload: Decodable, Equatable, Sendable, Identifiable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let replicas: Int
    public let cpuRequest: Double
    public let memoryRequest: Double
    public let priorityClassName: String
    public let priority: Int
    public let noPriorityClass: Bool
    public let qos: QosClass
    public let oomKills: Int
    public let risk: CapacityRisk
    public let moreFit: Int
    public let moreFitNA: Bool

    public var id: String { "\(kind)/\(namespace)/\(name)" }
    public var atRisk: Bool { risk != .ok }
    public var isSystem: Bool { isSystemClass(priorityClassName, priority: priority) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        replicas = try c.field(.replicas, 0)
        cpuRequest = try c.field(.cpuRequest, 0)
        memoryRequest = try c.field(.memoryRequest, 0)
        priorityClassName = try c.field(.priorityClassName, "")
        priority = try c.field(.priority, 0)
        noPriorityClass = try c.field(.noPriorityClass, false)
        qos = try c.wire(.qos)
        oomKills = try c.field(.oomKills, 0)
        risk = try c.wire(.risk)
        moreFit = try c.field(.moreFit, 0)
        moreFitNA = try c.field(.moreFitNA, false)
    }

    private enum CodingKeys: String, CodingKey {
        case kind, namespace, name, replicas, cpuRequest, memoryRequest, priorityClassName, priority, noPriorityClass, qos, oomKills, risk
        case moreFit, moreFitNA
    }
}

/// The workloads a filter keeps, in the order given.
public enum CapacityFilter: String, CaseIterable, Sendable {
    case all, atRisk, noPriorityClass
}

public extension Array where Element == CapacityWorkload {
    func filtered(_ filter: CapacityFilter) -> [CapacityWorkload] {
        switch filter {
        case .all: self
        case .atRisk: self.filter(\.atRisk)
        case .noPriorityClass: self.filter(\.noPriorityClass)
        }
    }
}

/// OK when every node's pods would fit on the others; otherwise `node` is the first that would not.
public struct NPlusOne: Decodable, Equatable, Sendable {
    public var ok = true
    public var node = ""
    public var pendingPods = 0

    public init() {}

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.field(.ok, true)
        node = try c.field(.node, "")
        pendingPods = try c.field(.pendingPods, 0)
    }

    private enum CodingKeys: String, CodingKey { case ok, node, pendingPods }
}

/// Losing `node` (Go `KubeCapacityNodeLoss`): where its pods land, what they evict, what stays
/// Pending and what to scale down, lowest priority first. `worstCase`: chosen as the worst of `candidates`.
public struct NodeLossPlan: Decodable, Equatable, Sendable {
    public let node: String
    public let worstCase: Bool
    public let cpuLost: Double
    public let memoryLost: Double
    public let podsLost: Int
    public let daemonSetPods: Int
    public let placed: [NodeLossPod]
    public let preempted: [NodeLossPod]
    public let pending: [NodeLossPod]
    public let suggestions: [NodeLossSuggestion]
    public let covered: Bool
    public let candidates: [NodeLossCandidate]
    public let assumptions: [String]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        worstCase = try c.field(.worstCase, false)
        cpuLost = try c.field(.cpuLost, 0)
        memoryLost = try c.field(.memoryLost, 0)
        podsLost = try c.field(.podsLost, 0)
        daemonSetPods = try c.field(.daemonSetPods, 0)
        placed = try c.field(.placed, [])
        preempted = try c.field(.preempted, [])
        pending = try c.field(.pending, [])
        suggestions = try c.field(.suggestions, [])
        covered = try c.field(.covered, true)
        candidates = try c.field(.candidates, [])
        assumptions = try c.field(.assumptions, [])
    }

    private enum CodingKeys: String, CodingKey {
        case node, worstCase, cpuLost, memoryLost, podsLost, daemonSetPods, placed, preempted, pending, suggestions, covered, candidates, assumptions
    }
}

/// A pod of the plan. Placed: `to`; preempted: evicted from `node` by `by` ("ns/pod"); Pending: `reason`.
public struct NodeLossPod: Decodable, Equatable, Sendable, Identifiable {
    public let namespace: String
    public let name: String
    public let workload: String
    public let priority: Int
    public let to: String
    public let node: String
    public let by: String
    public let pdbViolated: Bool
    /// cpu, memory or pods.
    public let reason: String

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        workload = try c.field(.workload, "")
        priority = try c.field(.priority, 0)
        to = try c.field(.to, "")
        node = try c.field(.node, "")
        by = try c.field(.by, "")
        pdbViolated = try c.field(.pdbViolated, false)
        reason = try c.field(.reason, "")
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, workload, priority, to, node, by, pdbViolated, reason }
}

/// Scale `kind`/`namespace`/`name` from `replicas` to `scaleTo`; `pdbBlocks`: its budget lets none go.
public struct NodeLossSuggestion: Decodable, Equatable, Sendable, Identifiable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let replicas: Int
    public let scaleTo: Int
    public let priority: Int
    public let priorityClassName: String
    public let noPriorityClass: Bool
    public let pdb: String
    public let pdbMinAvailable: Int
    public let pdbBlocks: Bool
    public let freesCPU: Double
    public let freesMemory: Double

    public var id: String { "\(kind)/\(namespace)/\(name)" }

    /// The resource browser's type of its workload, to open the scale sheet; nil for a kind it cannot scale.
    public var scaleResource: KubeAPIResource? {
        switch kind {
        case "Deployment": KubeAPIResource(group: "apps", resource: "deployments", kind: kind, scalable: true)
        case "StatefulSet": KubeAPIResource(group: "apps", resource: "statefulsets", kind: kind, scalable: true)
        case "ReplicaSet": KubeAPIResource(group: "apps", resource: "replicasets", kind: kind, scalable: true)
        case "Job": KubeAPIResource(group: "batch", resource: "jobs", kind: kind, scalable: true)
        default: nil
        }
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        replicas = try c.field(.replicas, 0)
        scaleTo = try c.field(.scaleTo, 0)
        priority = try c.field(.priority, 0)
        priorityClassName = try c.field(.priorityClassName, "")
        noPriorityClass = try c.field(.noPriorityClass, false)
        pdb = try c.field(.pdb, "")
        pdbMinAvailable = try c.field(.pdbMinAvailable, 0)
        pdbBlocks = try c.field(.pdbBlocks, false)
        let frees = try c.field(.frees, Frees())
        freesCPU = frees.cpu
        freesMemory = frees.memory
    }

    private struct Frees: Decodable {
        var cpu = 0.0
        var memory = 0.0

        init() {}

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            cpu = try c.field(.cpu, 0)
            memory = try c.field(.memory, 0)
        }

        private enum CodingKeys: String, CodingKey { case cpu, memory }
    }

    private enum CodingKeys: String, CodingKey {
        case kind, namespace, name, replicas, scaleTo, priority, priorityClassName, noPriorityClass, pdb, pdbMinAvailable, pdbBlocks, frees
    }
}

public struct NodeLossCandidate: Decodable, Equatable, Sendable, Identifiable {
    public let name: String
    public let pending: Int
    public let preempted: Int

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        pending = try c.field(.pending, 0)
        preempted = try c.field(.preempted, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, pending, preempted }
}
