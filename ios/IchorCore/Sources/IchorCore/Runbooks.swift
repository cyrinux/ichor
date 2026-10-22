import Foundation

// Runbooks (docs/runbooks.md): ordered lists of the app's own actions, kept per cluster by the
// Go core (sealed in its data directory) and run step by step, each destructive step after its
// own confirmation. These are the shapes Go answers and the rules the screens apply; the same
// as Android's model/Runbook.kt.

/// A stored runbook's row (RunbookStoreList, RunbookStoreImport): problem says why it no longer
/// loads, then it can only be removed.
public struct RunbookItem: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let description: String
    public let steps: Int
    public let destructive: Int
    public let importedAt: Int64
    public let problem: String

    private enum CodingKeys: String, CodingKey { case id, name, description, steps, destructive, importedAt, problem }

    public init(id: String, name: String, description: String = "", steps: Int = 0, destructive: Int = 0, importedAt: Int64 = 0, problem: String = "") {
        self.id = id
        self.name = name
        self.description = description
        self.steps = steps
        self.destructive = destructive
        self.importedAt = importedAt
        self.problem = problem
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        description = try c.field(.description, "")
        steps = try c.field(.steps, 0)
        destructive = try c.field(.destructive, 0)
        importedAt = try c.field(.importedAt, 0)
        problem = try c.field(.problem, "")
    }

    /// It loads: it can be planned and run.
    public var usable: Bool { problem.isEmpty }
}

/// RunbookStoreList's answer.
public struct RunbookList: Decodable, Equatable, Sendable {
    public let items: [RunbookItem]

    private enum CodingKeys: String, CodingKey { case items }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        items = try c.field(.items, [])
    }
}

/// RunbookSummary's answer: what the import shows before asking the cluster.
public struct RunbookSummary: Decodable, Equatable, Sendable {
    public let name: String
    public let description: String
    public let steps: Int
    public let destructive: Int

    private enum CodingKeys: String, CodingKey { case name, description, steps, destructive }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        description = try c.field(.description, "")
        steps = try c.field(.steps, 0)
        destructive = try c.field(.destructive, 0)
    }
}

/// What a step acts on: a Kubernetes object (kind, namespace, name) or a node.
public struct RunbookTarget: Decodable, Equatable, Hashable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let node: String

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, node }

    public init(kind: String = "", namespace: String = "", name: String = "", node: String = "") {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.node = node
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        node = try c.field(.node, "")
    }

    public var isNode: Bool { !node.isEmpty }

    /// "node 10.5.0.2", "Deployment demo-shop/ingest-api", "demo-shop/store-compaction".
    public var label: String {
        if isNode { return node }
        let object = "\(namespace)/\(name)"
        return kind.isEmpty ? object : "\(kind) \(object)"
    }
}

/// One step of RunbookPlan: denied is why the role or the cluster's RBAC refuses it.
public struct RunbookPlanStep: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let action: String
    public let target: RunbookTarget
    public let destructive: Bool
    public let continueOnError: Bool
    public let role: String
    public let allowed: Bool
    public let denied: String

    private enum CodingKeys: String, CodingKey { case id, action, target, destructive, continueOnError, role, allowed, denied }

    public init(id: String, action: String, target: RunbookTarget, destructive: Bool = false, continueOnError: Bool = false,
                role: String = "", allowed: Bool = true, denied: String = "") {
        self.id = id
        self.action = action
        self.target = target
        self.destructive = destructive
        self.continueOnError = continueOnError
        self.role = role
        self.allowed = allowed
        self.denied = denied
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        action = try c.field(.action, "")
        target = try c.field(.target, RunbookTarget())
        destructive = try c.field(.destructive, false)
        continueOnError = try c.field(.continueOnError, false)
        role = try c.field(.role, "")
        allowed = try c.field(.allowed, true)
        denied = try c.field(.denied, "")
    }

    /// How its run is confirmed: a node step by typing the node, a Kubernetes one plainly;
    /// nil for a step that runs without asking.
    public var confirmation: RunbookConfirmation? {
        guard destructive else { return nil }
        return target.isNode ? .typedHostname(target.node) : .plain
    }
}

/// How a destructive step is confirmed before it runs.
public enum RunbookConfirmation: Equatable, Hashable, Sendable {
    case typedHostname(String)
    case plain
}

/// RunbookPlan's answer.
public struct RunbookPlan: Decodable, Equatable, Sendable {
    public let name: String
    public let description: String
    public let steps: [RunbookPlanStep]
    public let errors: [String]

    private enum CodingKeys: String, CodingKey { case name, description, steps, errors }

    public init(name: String, description: String = "", steps: [RunbookPlanStep], errors: [String] = []) {
        self.name = name
        self.description = description
        self.steps = steps
        self.errors = errors
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        description = try c.field(.description, "")
        steps = try c.field(.steps, [])
        errors = try c.field(.errors, [])
    }

    public var destructiveCount: Int { steps.filter(\.destructive).count }

    /// The steps refused that the run would stop at: neither continueOnError in the YAML nor
    /// chosen to go past (`continueIDs`).
    public func blocking(continueIDs: Set<String>) -> [RunbookPlanStep] {
        steps.filter { !$0.allowed && !$0.continueOnError && !continueIDs.contains($0.id) }
    }

    /// Run is offered: there are steps, no plan error, and no refused step it would stop at.
    public func canRun(continueIDs: Set<String>) -> Bool {
        !steps.isEmpty && errors.isEmpty && blocking(continueIDs: continueIDs).isEmpty
    }
}

/// Where a step stands in a run.
public enum RunbookStepPhase: String, Decodable, Equatable, Hashable, Sendable {
    /// A destructive step waiting for its confirmation.
    case waiting
    case running, ok, failed, skipped
    /// Not reached yet (no state from Go).
    case pending

    public var ended: Bool { self == .ok || self == .failed || self == .skipped }
}

/// A step's state as StartRunbook's listener reports it.
public struct RunbookStepState: Decodable, Equatable, Hashable, Sendable {
    public let index: Int
    public let id: String
    public let state: RunbookStepPhase
    public let message: String
    public let startedAt: Int64
    public let endedAt: Int64

    private enum CodingKeys: String, CodingKey { case index, id, state, message, startedAt, endedAt }

    public init(index: Int, id: String, state: RunbookStepPhase, message: String = "", startedAt: Int64 = 0, endedAt: Int64 = 0) {
        self.index = index
        self.id = id
        self.state = state
        self.message = message
        self.startedAt = startedAt
        self.endedAt = endedAt
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        index = try c.field(.index, 0)
        id = try c.field(.id, "")
        // A state this app does not know yet shows as not reached rather than failing the run.
        state = RunbookStepPhase(rawValue: try c.field(.state, "")) ?? .pending
        message = try c.field(.message, "")
        startedAt = try c.field(.startedAt, 0)
        endedAt = try c.field(.endedAt, 0)
    }
}

/// StartRunbook's final answer.
public struct RunbookResult: Decodable, Equatable, Sendable {
    public let ok: Bool
    public let steps: [RunbookStepState]

    private enum CodingKeys: String, CodingKey { case ok, steps }

    public init(ok: Bool, steps: [RunbookStepState]) {
        self.ok = ok
        self.steps = steps
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.field(.ok, false)
        steps = try c.field(.steps, [])
    }
}

/// A run's progress as its screen and Live Activity show it.
public struct RunbookProgress: Equatable, Sendable {
    /// Each step's latest state, in the plan's order (pending until Go reports it).
    public private(set) var steps: [RunbookStepState]

    public init(plan: RunbookPlan) {
        steps = plan.steps.enumerated().map { RunbookStepState(index: $0.offset, id: $0.element.id, state: .pending) }
    }

    /// Takes a step's new state; one out of range is ignored.
    public mutating func apply(_ state: RunbookStepState) {
        guard steps.indices.contains(state.index) else { return }
        steps[state.index] = state
    }

    /// Takes the final states.
    public mutating func apply(_ result: RunbookResult) {
        for state in result.steps { apply(state) }
    }

    /// The step waiting for its confirmation, if one is.
    public var waiting: RunbookStepState? { steps.first { $0.state == .waiting } }

    /// The step under way (waiting or running), else the last that ended.
    public var current: RunbookStepState? {
        steps.first { $0.state == .waiting || $0.state == .running } ?? steps.last { $0.state.ended }
    }

    /// "Step N of M": N from 1, the step under way (or the last ended); nil before any.
    public var position: (step: Int, total: Int)? {
        guard let current else { return nil }
        return (current.index + 1, steps.count)
    }

    /// Steps ended, for the bar.
    public var ended: Int { steps.filter { $0.state.ended }.count }
}

/// The ids chosen to go past, as StartRunbook takes them.
public func runbookContinueIDs(_ ids: Set<String>) -> String {
    ids.sorted().joined(separator: ",")
}
