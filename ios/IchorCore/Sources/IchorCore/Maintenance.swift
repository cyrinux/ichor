import Foundation

/// What a node maintenance does after the drain (StartNodeMaintenance's action).
public enum MaintenanceAction: String, CaseIterable, Identifiable, Sendable {
    case reboot, shutdown, none

    public var id: String { rawValue }

    /// The plan's blockers and acknowledgments concern the node going down: a drain alone
    /// ignores them.
    public var takesNodeDown: Bool { self != .none }
}

/// One pod of the node in a maintenance plan or run (drainPod in Go).
public struct DrainPod: Decodable, Equatable, Identifiable, Sendable {
    public enum Kind: String, Sendable {
        /// Evicted: a controller recreates it elsewhere.
        case evict
        /// No controller: evicted only when the user allows it, never recreated.
        case bare
        /// Left alone: its DaemonSet would recreate it on the node.
        case daemonset
        /// Left alone: a static pod of the node.
        case staticPod = "static"
    }

    public enum State: String, Sendable {
        case pending, evicting, blocked, gone
    }

    public let namespace: String
    public let name: String
    /// "ReplicaSet/web-5d8f", "" when none.
    public let owner: String
    /// Go's kind; an unknown one is treated as left alone.
    public let kind: String
    /// Keeps data in an emptyDir volume, lost when evicted.
    public let emptyDir: Bool
    /// The PodDisruptionBudget covering the pod, "" when none.
    public let pdb: String
    /// Disruptions the budget allows now, -1 without a budget.
    public let pdbAllowed: Int
    /// During a run: pending, evicting, blocked or gone ("" in a plan).
    public let state: String
    /// Why the pod is blocked, "" otherwise.
    public let reason: String

    public var id: String { "\(namespace)/\(name)" }
    public var podKind: Kind? { Kind(rawValue: kind) }
    public var podState: State? { State(rawValue: state) }
    public var hasPDB: Bool { !pdb.isEmpty && pdbAllowed >= 0 }
    /// The budget allows no disruption now: the drain waits for it.
    public var pdbBlocks: Bool { hasPDB && pdbAllowed == 0 }

    public init(namespace: String, name: String, owner: String = "", kind: String = Kind.evict.rawValue, emptyDir: Bool = false,
                pdb: String = "", pdbAllowed: Int = -1, state: String = "", reason: String = "") {
        self.namespace = namespace
        self.name = name
        self.owner = owner
        self.kind = kind
        self.emptyDir = emptyDir
        self.pdb = pdb
        self.pdbAllowed = pdbAllowed
        self.state = state
        self.reason = reason
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, owner, kind, emptyDir, pdb, pdbAllowed, state, reason }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.decodeIfPresent(String.self, forKey: .namespace) ?? ""
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        owner = try c.decodeIfPresent(String.self, forKey: .owner) ?? ""
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        emptyDir = try c.decodeIfPresent(Bool.self, forKey: .emptyDir) ?? false
        pdb = try c.decodeIfPresent(String.self, forKey: .pdb) ?? ""
        pdbAllowed = try c.decodeIfPresent(Int.self, forKey: .pdbAllowed) ?? -1
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? ""
        reason = try c.decodeIfPresent(String.self, forKey: .reason) ?? ""
    }
}

/// The pods of a plan by what the drain does with them.
public struct MaintenancePodGroups: Equatable, Sendable {
    /// Evicted (a controller recreates them).
    public let toEvict: [DrainPod]
    /// Without a controller: evicted only when allowed, never recreated.
    public let bare: [DrainPod]
    /// DaemonSet and static pods (and unknown kinds): they stay on the node.
    public let leftAlone: [DrainPod]

    public init(_ pods: [DrainPod]) {
        toEvict = pods.filter { $0.podKind == .evict }
        bare = pods.filter { $0.podKind == .bare }
        leftAlone = pods.filter { $0.podKind != .evict && $0.podKind != .bare }
    }
}

/// What a maintenance of one node would do and what stands in the way (NodeMaintenancePlan).
public struct MaintenancePlan: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    /// The Kubernetes node name.
    public let kubeNode: String
    public let controlPlane: Bool
    public let cordoned: Bool
    public let pods: [DrainPod]
    /// Refuse a reboot or shutdown (not a drain alone).
    public let blockers: [String]
    public let warnings: [String]
    /// Confirmed one by one before a reboot or shutdown.
    public let acknowledge: [String]

    public var podGroups: MaintenancePodGroups { MaintenancePodGroups(pods) }

    public init(node: String, hostname: String = "", kubeNode: String = "", controlPlane: Bool = false, cordoned: Bool = false,
                pods: [DrainPod] = [], blockers: [String] = [], warnings: [String] = [], acknowledge: [String] = []) {
        self.node = node
        self.hostname = hostname
        self.kubeNode = kubeNode
        self.controlPlane = controlPlane
        self.cordoned = cordoned
        self.pods = pods
        self.blockers = blockers
        self.warnings = warnings
        self.acknowledge = acknowledge
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, kubeNode, controlPlane, cordoned, pods, blockers, warnings, acknowledge }

    // Go encodes empty slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decodeIfPresent(String.self, forKey: .node) ?? ""
        hostname = try c.decodeIfPresent(String.self, forKey: .hostname) ?? ""
        kubeNode = try c.decodeIfPresent(String.self, forKey: .kubeNode) ?? ""
        controlPlane = try c.decodeIfPresent(Bool.self, forKey: .controlPlane) ?? false
        cordoned = try c.decodeIfPresent(Bool.self, forKey: .cordoned) ?? false
        pods = try c.decodeIfPresent([DrainPod].self, forKey: .pods) ?? []
        blockers = try c.decodeIfPresent([String].self, forKey: .blockers) ?? []
        warnings = try c.decodeIfPresent([String].self, forKey: .warnings) ?? []
        acknowledge = try c.decodeIfPresent([String].self, forKey: .acknowledge) ?? []
    }

    /// The acknowledgments the user must tick for `action` (none for a drain alone).
    public func acknowledgments(for action: MaintenanceAction) -> [String] {
        action.takesNodeDown ? acknowledge : []
    }

    /// Go's rules: a drain alone always runs; a reboot or shutdown needs no blocker and every
    /// acknowledgment ticked.
    public func canStart(action: MaintenanceAction, acknowledged: Set<String>) -> Bool {
        guard action.takesNodeDown else { return true }
        return blockers.isEmpty && Set(acknowledge).isSubset(of: acknowledged)
    }
}

/// Phases of a maintenance run, in order.
public enum MaintenancePhase: String, CaseIterable, Comparable, Sendable {
    case cordon, drain, reboot, shutdown, waiting, uncordon

    /// The phases a run of `action` goes through.
    public static func steps(for action: MaintenanceAction) -> [MaintenancePhase] {
        switch action {
        case .reboot: [.cordon, .drain, .reboot, .waiting, .uncordon]
        case .shutdown: [.cordon, .drain, .shutdown]
        case .none: [.cordon, .drain]
        }
    }

    public static func < (a: Self, b: Self) -> Bool {
        allCases.firstIndex(of: a)! < allCases.firstIndex(of: b)!
    }
}

/// One OnProgress event of a maintenance run: `at` is unix ms; `pods` the pods being evicted
/// with their state (empty outside the drain).
public struct MaintenanceProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let message: String
    public let at: Int64
    public let pods: [DrainPod]

    public init(phase: String, message: String = "", at: Int64 = 0, pods: [DrainPod] = []) {
        self.phase = phase
        self.message = message
        self.at = at
        self.pods = pods
    }

    private enum CodingKeys: String, CodingKey { case phase, message, at, pods }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.decodeIfPresent(String.self, forKey: .phase) ?? ""
        message = try c.decodeIfPresent(String.self, forKey: .message) ?? ""
        at = try c.decodeIfPresent(Int64.self, forKey: .at) ?? 0
        pods = try c.decodeIfPresent([DrainPod].self, forKey: .pods) ?? []
    }
}

/// A row of the maintenance timeline.
public struct MaintenanceStep: Equatable, Identifiable, Sendable {
    public let phase: MaintenancePhase
    public let state: UpgradeStepState
    /// Unix ms the phase was first reported, 0 when not reached.
    public let at: Int64
    /// Latest message of the phase.
    public let message: String

    public var id: MaintenancePhase { phase }

    public init(phase: MaintenancePhase, state: UpgradeStepState, at: Int64 = 0, message: String = "") {
        self.phase = phase
        self.state = state
        self.at = at
        self.message = message
    }
}

/// Timeline of `action`'s phases from the events so far, like upgradeTimeline: phases only
/// move forward, `finished` marks them all done, `failure` marks the furthest one failed.
public func maintenanceTimeline(_ events: [MaintenanceProgress], action: MaintenanceAction, finished: Bool = false,
                                failure: String? = nil) -> [MaintenanceStep] {
    let phases = MaintenancePhase.steps(for: action)
    var first: [MaintenancePhase: Int64] = [:]
    var messages: [MaintenancePhase: String] = [:]
    var furthest: MaintenancePhase?
    for event in events {
        guard let phase = MaintenancePhase(rawValue: event.phase), phases.contains(phase) else { continue }
        if first[phase] == nil { first[phase] = event.at }
        if !event.message.isEmpty { messages[phase] = event.message }
        if furthest.map({ phase > $0 }) ?? true { furthest = phase }
    }
    return phases.map { phase in
        let at = first[phase] ?? 0
        let message = messages[phase] ?? ""
        if let failure {
            let failed = furthest ?? phases[0]
            if phase < failed { return MaintenanceStep(phase: phase, state: .done, at: at, message: message) }
            if phase == failed { return MaintenanceStep(phase: phase, state: .failed, at: at, message: failure) }
            return MaintenanceStep(phase: phase, state: .pending)
        }
        if finished { return MaintenanceStep(phase: phase, state: .done, at: at, message: message) }
        guard let furthest else { return MaintenanceStep(phase: phase, state: .pending) }
        let state: UpgradeStepState = phase < furthest ? .done : phase == furthest ? .current : .pending
        return MaintenanceStep(phase: phase, state: state, at: at, message: message)
    }
}

/// The pods of the latest event that listed them (the drain's live states).
public func latestDrainPods(_ events: [MaintenanceProgress]) -> [DrainPod] {
    events.last { !$0.pods.isEmpty }?.pods ?? []
}
