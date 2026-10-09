import Foundation

// Mirrors go/ichorgo/kube_castai_plans.go (and the Android model/DataServicesCastAIPlans.kt).

/// One node consolidation CAST AI ran (a RebalancePlan): the nodes it removes and adds, and what it costs.
public struct CastAIPlan: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// Unix ms.
    public let createdAt: Int64
    /// When it finished or failed, 0 while it runs.
    public let endedAt: Int64
    /// full, delete-empty, drain-only.
    public let mode: CastAIPlanMode
    /// As CAST AI writes it: Pending, Created, Running, Done, Failed, Skipped, Canceled, Expired.
    public let state: String
    /// Executed without approval; false waits for one.
    public let execute: Bool
    public let currency: String
    /// Monthly cost of the nodes it touches, before and after.
    public let beforeMonthly: Double
    public let afterMonthly: Double
    public let savingsPercent: Double
    /// What it actually saved per month, when CAST AI measured it.
    public let achievedMonthly: Double?
    public let clusterMonthly: Double
    public let clusterNodes: Int
    /// A failed plan's monthly saving lost: the cost of the nodes it did not remove.
    public let missedMonthly: Double
    /// missedMonthly is the planned saving shared out by nodes left: a node could not be priced.
    public let missedEstimated: Bool
    public let failureReason: String
    /// Creation or Deletion.
    public let failurePhase: String
    /// The failure or skip message, in CAST AI's words.
    public let message: String
    public let warnings: [String]
    public let removing: [CastAIPlanNode]
    public let adding: [CastAIPlanNode]
    public let budgets: [CastAINodeBudget]

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        createdAt = try c.field(.createdAt, 0)
        endedAt = try c.field(.endedAt, 0)
        mode = try c.wire(.mode)
        state = try c.field(.state, "")
        execute = try c.field(.execute, false)
        currency = try c.field(.currency, "")
        beforeMonthly = try c.field(.beforeMonthly, 0)
        afterMonthly = try c.field(.afterMonthly, 0)
        savingsPercent = try c.field(.savingsPercent, 0)
        achievedMonthly = try c.decodeIfPresent(Double.self, forKey: .achievedMonthly)
        clusterMonthly = try c.field(.clusterMonthly, 0)
        clusterNodes = try c.field(.clusterNodes, 0)
        missedMonthly = try c.field(.missedMonthly, 0)
        missedEstimated = try c.field(.missedEstimated, false)
        failureReason = try c.field(.failureReason, "")
        failurePhase = try c.field(.failurePhase, "")
        message = try c.field(.message, "")
        warnings = try c.field(.warnings, [])
        removing = try c.field(.removing, [])
        adding = try c.field(.adding, [])
        budgets = try c.field(.budgets, [])
    }

    public var planState: CastAIPlanState { CastAIPlanState(state: state, execute: execute) }

    /// The monthly saving it aims for.
    public var plannedMonthly: Double { max(beforeMonthly - afterMonthly, 0) }

    /// The monthly saving it made: measured when CAST AI did, else planned; nil unless it finished.
    public var savedMonthly: Double? { planState == .done ? (achievedMonthly ?? plannedMonthly) : nil }

    /// "2 of 3": the nodes done out of those listed.
    public var removed: Int { removing.filter { $0.status == .success }.count }
    public var added: Int { adding.filter { $0.status == .success }.count }

    /// The planned saving as a share of the whole cluster's monthly cost, nil when that is unknown.
    public var clusterSharePercent: Double? { clusterMonthly > 0 ? plannedMonthly / clusterMonthly * 100 : nil }

    private enum CodingKeys: String, CodingKey {
        case name, createdAt, endedAt, mode, state, execute, currency, beforeMonthly, afterMonthly, savingsPercent, achievedMonthly
        case clusterMonthly, clusterNodes, missedMonthly, missedEstimated, failureReason, failurePhase, message, warnings
        case removing, adding, budgets
    }
}

public struct CastAIPlanNode: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let status: CastAINodeStatus
    public let instanceType: String
    public let spot: Bool
    public let zone: String
    public let priceHourly: Double
    public let events: [CastAIPlanEvent]

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        status = try c.wire(.status)
        instanceType = try c.field(.instanceType, "")
        spot = try c.field(.spot, false)
        zone = try c.field(.zone, "")
        priceHourly = try c.field(.priceHourly, 0)
        events = try c.field(.events, [])
    }

    /// How long an added node took to be ready (first to last step, in seconds); nil unless it succeeded.
    public var readySeconds: Int64? {
        guard status == .success, let first = events.first, let last = events.last, last.at >= first.at else { return nil }
        return (last.at - first.at) / 1000
    }

    private enum CodingKeys: String, CodingKey { case name, status, instanceType, spot, zone, priceHourly, events }
}

/// One step of a node's creation or deletion: CAST AI's status (NodeCordoned, Blocked, Success...) and words.
public struct CastAIPlanEvent: Decodable, Equatable, Sendable {
    public let at: Int64
    public let status: String
    public let description: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.field(.at, 0)
        status = try c.field(.status, "")
        description = try c.field(.description, "")
    }

    /// CAST AI's words for the step, its status when it has none.
    public var text: String { description.isEmpty ? status : description }

    /// Done, failed or given back, waiting, moving: how the step reads.
    public var tone: CastAIEventTone {
        switch status {
        case "Success": .ok
        case "Failed", "NodeUncordoned": .bad
        case "Blocked": .warn
        case "InProgress": .moving
        default: .neutral
        }
    }

    private enum CodingKeys: String, CodingKey { case at, status, description }
}

public enum CastAIEventTone: Sendable {
    case ok, bad, warn, moving, neutral
}

/// A NodePool's disruption budget: how many of its nodes may be disrupted at once.
public struct CastAINodeBudget: Decodable, Equatable, Identifiable, Sendable {
    public let nodePool: String
    public let allowed: Int
    public let disrupting: Int
    public let nodes: Int

    public var id: String { nodePool }
    /// No more node may be disrupted now.
    public var exhausted: Bool { disrupting >= allowed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodePool = try c.field(.nodePool, "")
        allowed = try c.field(.allowed, 0)
        disrupting = try c.field(.disrupting, 0)
        nodes = try c.field(.nodes, 0)
    }

    private enum CodingKeys: String, CodingKey { case nodePool, allowed, disrupting, nodes }
}

/// A node a plan failed to remove that a later plan tried again; retrying while one runs.
public struct CastAIStuckNode: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let failures: Int
    public let retrying: Bool

    public var id: String { node }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        failures = try c.field(.failures, 0)
        retrying = try c.field(.retrying, false)
    }

    private enum CodingKeys: String, CodingKey { case node, failures, retrying }
}

public enum CastAIPlanState: Hashable, Sendable, CaseIterable {
    case awaitingApproval, pending, running, done, failed, skipped

    public init(state: String, execute: Bool) {
        switch state {
        case "Running": self = .running
        case "Done": self = .done
        case "Failed": self = .failed
        case "Skipped", "Canceled", "Expired": self = .skipped
        default: self = execute ? .pending : .awaitingApproval
        }
    }
}

public enum CastAIPlanMode: String, Hashable, Sendable, WireEnum {
    case full, deleteEmpty = "delete-empty", drainOnly = "drain-only", other = ""

    public static var wireFallback: CastAIPlanMode { .other }
}

public enum CastAINodeStatus: String, Hashable, Sendable, WireEnum {
    case pending, inProgress, blocked, success, failed

    public static var wireFallback: CastAINodeStatus { .pending }
}

/// The plans of the last window: what finished ones saved, what failed ones did not, by state.
public struct CastAIPlanSummary: Equatable, Sendable {
    public let currency: String
    public let savedMonthly: Double
    /// Some of savedMonthly is planned, not measured by CAST AI.
    public let savedEstimated: Bool
    /// What failed plans did not save: only the nodes they left.
    public let missedMonthly: Double
    public let missedEstimated: Bool
    public let done: Int
    public let failed: Int
    public let running: Int
    public let other: Int
    public let clusterMonthly: Double
    public let clusterNodes: Int
}

public let castAISummaryWindowMillis: Int64 = 24 * 60 * 60 * 1000

public extension CastAIStatus {
    func planSummary(now: Int64, windowMillis: Int64 = castAISummaryWindowMillis) -> CastAIPlanSummary {
        let recent = plans.filter { $0.createdAt >= now - windowMillis }
        func count(_ states: CastAIPlanState...) -> Int { recent.filter { states.contains($0.planState) }.count }
        let done = recent.filter { $0.planState == .done }
        let failed = recent.filter { $0.planState == .failed }
        return CastAIPlanSummary(
            currency: plans.first { !$0.currency.isEmpty }?.currency ?? "",
            savedMonthly: done.reduce(0) { $0 + ($1.savedMonthly ?? 0) },
            savedEstimated: done.contains { $0.achievedMonthly == nil && $0.plannedMonthly > 0 },
            missedMonthly: failed.reduce(0) { $0 + $1.missedMonthly },
            missedEstimated: failed.contains(where: \.missedEstimated),
            done: done.count,
            failed: failed.count,
            running: count(.running, .pending),
            other: count(.skipped, .awaitingApproval),
            clusterMonthly: plans.first?.clusterMonthly ?? 0,
            clusterNodes: plans.first?.clusterNodes ?? 0
        )
    }

    /// Plans grouped for the list: waiting and running first, then failed, then finished, each newest first.
    var planGroups: [CastAIPlanGroup] {
        let order: [CastAIPlanState] = [.awaitingApproval, .running, .pending, .failed, .done, .skipped]
        return order.compactMap { state in
            let group = plans.filter { $0.planState == state }.sorted { $0.createdAt > $1.createdAt }
            return group.isEmpty ? nil : CastAIPlanGroup(state: state, plans: group)
        }
    }

    /// The highest cost before any plan: one scale for every plan's bar.
    var planCostScale: Double { plans.map(\.beforeMonthly).max() ?? 0 }
}

public struct CastAIPlanGroup: Equatable, Identifiable, Sendable {
    public let state: CastAIPlanState
    public let plans: [CastAIPlan]
    public var id: CastAIPlanState { state }
}
