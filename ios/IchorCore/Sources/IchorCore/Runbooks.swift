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

    public init(
        id: String, name: String, description: String = "", steps: Int = 0, destructive: Int = 0, importedAt: Int64 = 0,
        problem: String = ""
    ) {
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

/// A value a runbook asks for before its plan (RunbookSummary's params).
public struct RunbookParam: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public static let kindNamespace = "namespace"
    public static let kindWorkload = "workload"
    public static let kindNode = "node"
    public static let kindInt = "int"
    public static let kindBool = "bool"

    public let name: String
    public let prompt: String
    public let defaultValue: String
    public let kind: String

    public var id: String { name }

    private enum CodingKeys: String, CodingKey {
        case name, prompt, kind
        case defaultValue = "default"
    }

    public init(name: String, prompt: String = "", defaultValue: String = "", kind: String = "string") {
        self.name = name
        self.prompt = prompt.isEmpty ? name : prompt
        self.defaultValue = defaultValue
        self.kind = kind
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        let asked: String = try c.field(.prompt, "")
        prompt = asked.isEmpty ? name : asked
        defaultValue = try c.field(.defaultValue, "")
        kind = try c.field(.kind, "string")
    }

    /// The names among `params` that have neither a value nor a default.
    public static func missing(_ params: [RunbookParam], values: [String: String]) -> [String] {
        params.filter { param in
            (values[param.name] ?? "").trimmingCharacters(in: .whitespaces).isEmpty && param.defaultValue.isEmpty
        }.map(\.name)
    }

    /// What the form starts from: the `remembered` value of a parameter that still exists, else its default.
    public static func initialValues(_ params: [RunbookParam], remembered: [String: String]) -> [String: String] {
        var values: [String: String] = [:]
        for param in params {
            let kept = (remembered[param.name] ?? "").trimmingCharacters(in: .whitespaces)
            values[param.name] = kept.isEmpty ? param.defaultValue : kept
        }
        return values
    }

    /// The values as a run's header shows them: "count=2 · ns=demo-shop", in a stable order.
    public static func label(_ values: [String: String]) -> String {
        values.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }.joined(separator: " · ")
    }
}

/// RunbookSummary's answer: what the import shows before asking the cluster.
public struct RunbookSummary: Decodable, Equatable, Sendable {
    public let name: String
    public let description: String
    public let steps: Int
    public let destructive: Int
    public let params: [RunbookParam]

    private enum CodingKeys: String, CodingKey { case name, description, steps, destructive, params }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        description = try c.field(.description, "")
        steps = try c.field(.steps, 0)
        destructive = try c.field(.destructive, 0)
        params = try c.field(.params, [])
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

    /// "node 10.5.0.2", "Deployment demo-shop/ingest-api", "demo-shop/store-compaction",
    /// "Pool shared" for a plugin's cluster-scoped object, or "Cluster" for a whole-cluster check.
    public var label: String {
        if isNode { return node }
        if namespace.isEmpty && name.isEmpty { return kind }
        let object = namespace.isEmpty ? name : "\(namespace)/\(name)"
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
    /// What a wait or assert step checks ("wait up to 5 min for ingest-api to be available"),
    /// empty for the other actions.
    public let detail: String
    /// How long a wait step may wait; 0 for the other actions.
    public let timeoutSeconds: Int
    /// A plugin step's plugin; empty for the other actions.
    public let pluginID: String
    public let pluginName: String
    public let pluginIcon: String
    /// A plugin step's action label, localised by the core.
    public let label: String
    /// The confirmation a plugin step's card asks for: none, plain or typedName.
    public let confirm: String

    private enum CodingKeys: String, CodingKey {
        case id, action, target, destructive, continueOnError, role, allowed, denied, detail, timeoutSeconds
        case pluginID = "pluginId"
        case pluginName, pluginIcon, label, confirm
    }

    public init(
        id: String, action: String, target: RunbookTarget, destructive: Bool = false, continueOnError: Bool = false,
        role: String = "", allowed: Bool = true, denied: String = "", detail: String = "", timeoutSeconds: Int = 0,
        pluginID: String = "", pluginName: String = "", pluginIcon: String = "", label: String = "",
        confirm: String = ""
    ) {
        self.pluginID = pluginID
        self.pluginName = pluginName
        self.pluginIcon = pluginIcon
        self.label = label
        self.confirm = confirm
        self.id = id
        self.action = action
        self.target = target
        self.destructive = destructive
        self.continueOnError = continueOnError
        self.role = role
        self.allowed = allowed
        self.denied = denied
        self.detail = detail
        self.timeoutSeconds = timeoutSeconds
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
        detail = try c.field(.detail, "")
        timeoutSeconds = try c.field(.timeoutSeconds, 0)
        pluginID = try c.field(.pluginID, "")
        pluginName = try c.field(.pluginName, "")
        pluginIcon = try c.field(.pluginIcon, "")
        label = try c.field(.label, "")
        confirm = try c.field(.confirm, "")
    }

    public var isPlugin: Bool { !pluginID.isEmpty }

    /// The data service whose icon a plugin step shows, as a plugin's row does.
    public var pluginKind: DataServiceKind? {
        isPlugin ? DataServiceKind(rawValue: pluginIcon.isEmpty ? pluginID : pluginIcon) : nil
    }

    /// What a typed confirmation asks for: the node, or the object's name.
    public var confirmName: String { target.isNode ? target.node : target.name }

    /// How its run is confirmed: a node step by typing the node, a plugin step whose card asks
    /// for it by typing the object's name, the others plainly; nil for a step that runs without
    /// asking.
    public var confirmation: RunbookConfirmation? {
        guard destructive else { return nil }
        return target.isNode || confirm == "typedName" ? .typedHostname(confirmName) : .plain
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

    public init(
        index: Int, id: String, state: RunbookStepPhase, message: String = "", startedAt: Int64 = 0, endedAt: Int64 = 0
    ) {
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

/// The whole seconds a wait step has waited since `startedAt` (unix ms) at `now`, within
/// 0...timeoutSeconds. Same as Android's waitedSeconds.
public func runbookWaitedSeconds(startedAt: Int64, now: Int64, timeoutSeconds: Int) -> Int {
    Int(min(max((now - startedAt) / 1000, 0), Int64(max(timeoutSeconds, 0))))
}

/// Seconds as "m:ss".
public func runbookClock(_ seconds: Int) -> String {
    let rest = seconds % 60
    return "\(seconds / 60):\(rest < 10 ? "0" : "")\(rest)"
}
