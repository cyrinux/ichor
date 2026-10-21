import Foundation

/// One Karpenter NodePool: its limits against what it runs, and how it may disrupt nodes.
public struct NodePoolRow: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let limits: NodePoolLimits
    public let resources: NodePoolResources
    public let atLimit: Bool
    public let disruption: NodePoolDisruption
    public let weight: Int
    public let ready: Bool
    public let message: String
    public let capacityTypes: [String]

    public var id: String { name }
    /// The state the row shows: at its limit, or not ready.
    public var state: AutoscaleState { atLimit ? .atMax : (ready ? .ok : .notReady) }

    public init(name: String = "", limits: NodePoolLimits = NodePoolLimits(), resources: NodePoolResources = NodePoolResources(),
                atLimit: Bool = false, disruption: NodePoolDisruption = NodePoolDisruption(), weight: Int = 0, ready: Bool = false,
                message: String = "", capacityTypes: [String] = []) {
        self.name = name
        self.limits = limits
        self.resources = resources
        self.atLimit = atLimit
        self.disruption = disruption
        self.weight = weight
        self.ready = ready
        self.message = message
        self.capacityTypes = capacityTypes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        limits = try c.field(.limits, NodePoolLimits())
        resources = try c.field(.resources, NodePoolResources())
        atLimit = try c.field(.atLimit, false)
        disruption = try c.field(.disruption, NodePoolDisruption())
        weight = try c.field(.weight, 0)
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        capacityTypes = try c.field(.capacityTypes, [])
    }

    private enum CodingKeys: String, CodingKey { case name, limits, resources, atLimit, disruption, weight, ready, message, capacityTypes }
}

public struct NodePoolLimits: Decodable, Equatable, Hashable, Sendable {
    public let cpu: String
    public let memory: String

    public init(cpu: String = "", memory: String = "") {
        self.cpu = cpu
        self.memory = memory
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cpu = try c.field(.cpu, "")
        memory = try c.field(.memory, "")
    }

    private enum CodingKeys: String, CodingKey { case cpu, memory }
}

public struct NodePoolResources: Decodable, Equatable, Hashable, Sendable {
    public let cpu: String
    public let memory: String
    public let nodes: Int

    public init(cpu: String = "", memory: String = "", nodes: Int = 0) {
        self.cpu = cpu
        self.memory = memory
        self.nodes = nodes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cpu = try c.field(.cpu, "")
        memory = try c.field(.memory, "")
        nodes = try c.field(.nodes, 0)
    }

    private enum CodingKeys: String, CodingKey { case cpu, memory, nodes }
}

public struct NodePoolDisruption: Decodable, Equatable, Hashable, Sendable {
    public let consolidationPolicy: String
    public let consolidateAfter: String
    public let budgets: [NodePoolBudget]

    public init(consolidationPolicy: String = "", consolidateAfter: String = "", budgets: [NodePoolBudget] = []) {
        self.consolidationPolicy = consolidationPolicy
        self.consolidateAfter = consolidateAfter
        self.budgets = budgets
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        consolidationPolicy = try c.field(.consolidationPolicy, "")
        consolidateAfter = try c.field(.consolidateAfter, "")
        budgets = try c.field(.budgets, [])
    }

    private enum CodingKeys: String, CodingKey { case consolidationPolicy, consolidateAfter, budgets }
}

public struct NodePoolBudget: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let nodes: String
    public let schedule: String
    public let duration: String
    public let reasons: [String]

    public var id: String { [nodes, schedule, duration].joined(separator: "/") }

    public init(nodes: String = "", schedule: String = "", duration: String = "", reasons: [String] = []) {
        self.nodes = nodes
        self.schedule = schedule
        self.duration = duration
        self.reasons = reasons
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, "")
        schedule = try c.field(.schedule, "")
        duration = try c.field(.duration, "")
        reasons = try c.field(.reasons, [])
    }

    private enum CodingKeys: String, CodingKey { case nodes, schedule, duration, reasons }
}

/// One Karpenter NodeClaim: a node being launched, running, or on its way out.
public struct NodeClaimRow: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let pool: String
    public let nodeName: String
    public let instanceType: String
    public let capacityType: String
    public let launched: Bool
    public let registered: Bool
    public let initialized: Bool
    public let ready: Bool
    public let message: String
    /// Unix ms.
    public let created: Int64
    public let pending: Bool
    public let drifted: Bool
    public let disrupting: Bool

    public var id: String { name }
    /// The state the row shows: pending, disrupting, drifted, or not ready.
    public var state: AutoscaleState {
        if pending { return .notReady }
        if disrupting { return .paused }
        if drifted { return .limited }
        return ready ? .ok : .notReady
    }

    public init(name: String = "", pool: String = "", nodeName: String = "", instanceType: String = "", capacityType: String = "",
                launched: Bool = false, registered: Bool = false, initialized: Bool = false, ready: Bool = false, message: String = "",
                created: Int64 = 0, pending: Bool = false, drifted: Bool = false, disrupting: Bool = false) {
        self.name = name
        self.pool = pool
        self.nodeName = nodeName
        self.instanceType = instanceType
        self.capacityType = capacityType
        self.launched = launched
        self.registered = registered
        self.initialized = initialized
        self.ready = ready
        self.message = message
        self.created = created
        self.pending = pending
        self.drifted = drifted
        self.disrupting = disrupting
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        pool = try c.field(.pool, "")
        nodeName = try c.field(.nodeName, "")
        instanceType = try c.field(.instanceType, "")
        capacityType = try c.field(.capacityType, "")
        launched = try c.field(.launched, false)
        registered = try c.field(.registered, false)
        initialized = try c.field(.initialized, false)
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        created = try c.field(.created, 0)
        pending = try c.field(.pending, false)
        drifted = try c.field(.drifted, false)
        disrupting = try c.field(.disrupting, false)
    }

    private enum CodingKeys: String, CodingKey {
        case name, pool, nodeName, instanceType, capacityType, launched, registered, initialized, ready, message, created, pending, drifted, disrupting
    }
}
