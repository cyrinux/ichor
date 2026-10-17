import Foundation

// A node's taints and labels (Go kube_node_meta.go): the read (KubeNodeMeta), the editor's
// draft and the edits document it sends, and the dry-run plan (NodeMetaPlan) shown before Apply.

/// The cordon's taint: Cordon / Uncordon own it, the editor never changes it (Go keeps the
/// server's copy).
public let nodeCordonTaintKey = "node.kubernetes.io/unschedulable"

/// The effect of a taint, in the order the editor offers them.
public enum NodeTaintEffect: String, CaseIterable, Identifiable, Sendable {
    case noSchedule = "NoSchedule"
    case preferNoSchedule = "PreferNoSchedule"
    case noExecute = "NoExecute"

    public var id: String { rawValue }
}

/// A taint of the node; `protected`: set by Kubernetes, Talos or the cloud provider.
public struct NodeTaint: Codable, Equatable, Hashable, Identifiable, Sendable {
    public let key: String
    public let value: String
    public let effect: String
    /// Unix seconds, 0 when the API server did not say.
    public let timeAdded: Int64
    public let protected: Bool

    public init(key: String, value: String = "", effect: String, timeAdded: Int64 = 0, protected: Bool = false) {
        self.key = key
        self.value = value
        self.effect = effect
        self.timeAdded = timeAdded
        self.protected = protected
    }

    /// Key and effect: a node has at most one taint for each pair.
    public var id: String { "\(key):\(effect)" }

    /// The cordon's taint, never edited here.
    public var isCordon: Bool { key == nodeCordonTaintKey }

    /// "key=value:Effect", as kubectl writes it.
    public var display: String { value.isEmpty ? "\(key):\(effect)" : "\(key)=\(value):\(effect)" }

    /// The same taint for the edits document: what the editor compares and sends.
    var edit: NodeTaintEdit { NodeTaintEdit(key: key, value: value, effect: effect) }

    private enum CodingKeys: String, CodingKey { case key, value, effect, timeAdded, protected }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        value = try c.field(.value, "")
        effect = try c.field(.effect, "")
        timeAdded = try c.field(.timeAdded, 0)
        protected = try c.field(.protected, false)
    }
}

/// A label of the node; `protected` as for NodeTaint.
public struct NodeLabel: Codable, Equatable, Hashable, Identifiable, Sendable {
    public let key: String
    public let value: String
    public let protected: Bool

    public init(key: String, value: String = "", protected: Bool = false) {
        self.key = key
        self.value = value
        self.protected = protected
    }

    public var id: String { key }

    private enum CodingKeys: String, CodingKey { case key, value, protected }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        value = try c.field(.value, "")
        protected = try c.field(.protected, false)
    }
}

/// The node's taints and labels as read (KubeNodeMeta / NodeMeta), sorted by key.
public struct KubeNodeMeta: Decodable, Equatable, Sendable {
    public let node: String
    /// The node's version when read: the edits carry it, so a node changed since fails the plan.
    public let resourceVersion: String
    public let taints: [NodeTaint]
    public let labels: [NodeLabel]

    public init(node: String, resourceVersion: String = "", taints: [NodeTaint] = [], labels: [NodeLabel] = []) {
        self.node = node
        self.resourceVersion = resourceVersion
        self.taints = taints
        self.labels = labels
    }

    private enum CodingKeys: String, CodingKey { case node, resourceVersion, taints, labels }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        resourceVersion = try c.field(.resourceVersion, "")
        taints = try c.field(.taints, [])
        labels = try c.field(.labels, [])
    }
}

/// A taint of the edits document: key, value, effect.
public struct NodeTaintEdit: Encodable, Equatable, Hashable, Sendable {
    public let key: String
    public let value: String
    public let effect: String

    public init(key: String, value: String = "", effect: String) {
        self.key = key
        self.value = value
        self.effect = effect
    }
}

/// The edits document, the same string for the plan and the apply: labels as deltas (a stale
/// copy never wipes the cloud's labels), taints as the full desired list without the cordon's.
/// `resourceVersion`: the read's, so a node changed since is reported rather than overwritten.
/// `force` (sent only when on): protected keys may change, for a plan that takes no force flag.
public struct NodeMetaEdits: Encodable, Equatable, Sendable {
    public let resourceVersion: String
    public let setLabels: [String: String]
    public let removeLabels: [String]
    public let taints: [NodeTaintEdit]
    public let force: Bool

    public init(resourceVersion: String = "", setLabels: [String: String] = [:], removeLabels: [String] = [],
                taints: [NodeTaintEdit] = [], force: Bool = false) {
        self.resourceVersion = resourceVersion
        self.setLabels = setLabels
        self.removeLabels = removeLabels
        self.taints = taints
        self.force = force
    }

    /// The same edits, with protected keys allowed or not.
    public func forcing(_ on: Bool) -> NodeMetaEdits {
        NodeMetaEdits(resourceVersion: resourceVersion, setLabels: setLabels, removeLabels: removeLabels, taints: taints, force: on)
    }

    private enum CodingKeys: String, CodingKey { case resourceVersion, setLabels, removeLabels, taints, force }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(resourceVersion, forKey: .resourceVersion)
        try c.encode(setLabels, forKey: .setLabels)
        try c.encode(removeLabels, forKey: .removeLabels)
        try c.encode(taints, forKey: .taints)
        if force { try c.encode(force, forKey: .force) }
    }

    /// The JSON string the Go core takes, keys sorted (stable for tests and logs).
    public func json() throws -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return String(decoding: try encoder.encode(self), as: UTF8.self)
    }
}

/// The editor's working copy of a node's taints and labels, compared to the read (`original`).
public struct NodeMetaDraft: Equatable, Sendable {
    public let original: KubeNodeMeta
    public private(set) var taints: [NodeTaint]
    public private(set) var labels: [NodeLabel]

    public init(_ meta: KubeNodeMeta) {
        original = meta
        taints = meta.taints
        labels = meta.labels
    }

    /// Whether the editor may change `taint`: never the cordon's, a protected one only when
    /// shown (`showProtected`, which then applies with force).
    public static func editable(_ taint: NodeTaint, showProtected: Bool) -> Bool {
        !taint.isCordon && (!taint.protected || showProtected)
    }

    public static func editable(_ label: NodeLabel, showProtected: Bool) -> Bool {
        !label.protected || showProtected
    }

    /// Adds `taint`, or replaces the one it edits (`replacing`) or that has its key and effect.
    public mutating func setTaint(_ taint: NodeTaint, replacing old: NodeTaint? = nil) {
        let kept = taints.filter { $0.id != taint.id && $0.id != old?.id }
        let protected = original.taints.first { $0.id == taint.id }?.protected ?? false
        let added = NodeTaint(key: taint.key, value: taint.value, effect: taint.effect, timeAdded: taint.timeAdded, protected: protected)
        taints = (kept + [added]).sorted { ($0.key, $0.effect) < ($1.key, $1.effect) }
    }

    public mutating func removeTaint(_ taint: NodeTaint) {
        taints = taints.filter { $0.id != taint.id }
    }

    /// Adds `label` or changes the value of the one with its key (or the edited `replacing`).
    public mutating func setLabel(_ label: NodeLabel, replacing old: NodeLabel? = nil) {
        let kept = labels.filter { $0.key != label.key && $0.key != old?.key }
        let protected = original.labels.first { $0.key == label.key }?.protected ?? false
        labels = (kept + [NodeLabel(key: label.key, value: label.value, protected: protected)]).sorted { $0.key < $1.key }
    }

    public mutating func removeLabel(_ label: NodeLabel) {
        labels = labels.filter { $0.key != label.key }
    }

    /// Back to the read.
    public mutating func reset() {
        taints = original.taints
        labels = original.labels
    }

    /// The desired taints for the edits document, the cordon's left out.
    private var desiredTaints: [NodeTaintEdit] { taints.filter { !$0.isCordon }.map(\.edit) }

    /// The taints differ from the read (the cordon's ignored, order ignored).
    public var hasTaintChanges: Bool {
        Set(desiredTaints) != Set(original.taints.filter { !$0.isCordon }.map(\.edit))
    }

    public var hasLabelChanges: Bool {
        !edits.setLabels.isEmpty || !edits.removeLabels.isEmpty
    }

    public var hasChanges: Bool { hasTaintChanges || hasLabelChanges }

    /// The edits document from the read to this draft.
    public var edits: NodeMetaEdits {
        let before = Dictionary(original.labels.map { ($0.key, $0.value) }, uniquingKeysWith: { first, _ in first })
        let after = Dictionary(labels.map { ($0.key, $0.value) }, uniquingKeysWith: { first, _ in first })
        let set = after.filter { before[$0.key] != $0.value }
        let removed = before.keys.filter { after[$0] == nil }.sorted()
        return NodeMetaEdits(resourceVersion: original.resourceVersion, setLabels: set, removeLabels: removed,
                             taints: desiredTaints)
    }

    /// Whether the edits touch a protected taint or label: they then need force.
    public var touchesProtected: Bool {
        let e = edits
        let protectedLabels = Set(original.labels.filter(\.protected).map(\.key))
        if !protectedLabels.isDisjoint(with: e.removeLabels) || !protectedLabels.isDisjoint(with: e.setLabels.keys) {
            return true
        }
        let desired = Set(e.taints)
        return original.taints.contains { $0.protected && !$0.isCordon && !desired.contains($0.edit) }
    }
}

/// One entry of a plan's diff: a label or taint added or removed (key, value, effect for a
/// taint), or changed (key, from, to).
public struct NodeMetaChange: Decodable, Equatable, Hashable, Sendable {
    public let key: String
    public let value: String
    public let effect: String
    public let from: String
    public let to: String

    public init(key: String, value: String = "", effect: String = "", from: String = "", to: String = "") {
        self.key = key
        self.value = value
        self.effect = effect
        self.from = from
        self.to = to
    }

    private enum CodingKeys: String, CodingKey { case key, value, effect, from, to }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        value = try c.field(.value, "")
        effect = try c.field(.effect, "")
        from = try c.field(.from, "")
        to = try c.field(.to, "")
    }

    /// "key=value" or "key=value:Effect" (added or removed); "key: from → to" (changed).
    public var display: String {
        if !from.isEmpty || !to.isEmpty {
            return effect.isEmpty ? "\(key): \(from) → \(to)" : "\(key):\(effect): \(from) → \(to)"
        }
        let pair = value.isEmpty ? key : "\(key)=\(value)"
        return effect.isEmpty ? pair : "\(pair):\(effect)"
    }
}

/// The added, removed and changed labels or taints of a plan.
public struct NodeMetaDiff: Decodable, Equatable, Sendable {
    public let added: [NodeMetaChange]
    public let removed: [NodeMetaChange]
    public let changed: [NodeMetaChange]

    public init(added: [NodeMetaChange] = [], removed: [NodeMetaChange] = [], changed: [NodeMetaChange] = []) {
        self.added = added
        self.removed = removed
        self.changed = changed
    }

    private enum CodingKeys: String, CodingKey { case added, removed, changed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        added = try c.field(.added, [])
        removed = try c.field(.removed, [])
        changed = try c.field(.changed, [])
    }

    public var isEmpty: Bool { added.isEmpty && removed.isEmpty && changed.isEmpty }
}

/// A pod a new NoExecute taint evicts at once.
public struct NodeMetaEviction: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let owner: String

    public init(namespace: String, name: String, owner: String = "") {
        self.namespace = namespace
        self.name = name
        self.owner = owner
    }

    public var id: String { "\(namespace)/\(name)" }

    private enum CodingKeys: String, CodingKey { case namespace, name, owner }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        owner = try c.field(.owner, "")
    }
}

/// The dry run of an edits document (KubeNodeMetaPlan / NodeMetaPlan): the diff, the pods a
/// NoExecute taint evicts, and what blocks or deserves a warning.
public struct NodeMetaPlan: Decodable, Equatable, Sendable {
    public let node: String
    public let labels: NodeMetaDiff
    public let taints: NodeMetaDiff
    public let evicts: [NodeMetaEviction]
    public let blockers: [String]
    public let warnings: [String]
    public let noChange: Bool

    public init(node: String, labels: NodeMetaDiff = NodeMetaDiff(), taints: NodeMetaDiff = NodeMetaDiff(),
                evicts: [NodeMetaEviction] = [], blockers: [String] = [], warnings: [String] = [], noChange: Bool = false) {
        self.node = node
        self.labels = labels
        self.taints = taints
        self.evicts = evicts
        self.blockers = blockers
        self.warnings = warnings
        self.noChange = noChange
    }

    private enum CodingKeys: String, CodingKey { case node, labels, taints, evicts, blockers, warnings, noChange }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        labels = try c.field(.labels, NodeMetaDiff())
        taints = try c.field(.taints, NodeMetaDiff())
        evicts = try c.field(.evicts, [])
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
        noChange = try c.field(.noChange, false)
    }

    /// A taint changes: the apply then asks for the typed hostname (labels only: a plain confirmation).
    public var hasTaintChanges: Bool { !taints.isEmpty }

    /// The node changed since the editor read it (Go says "…, reload"): offer to read it again.
    public var isStale: Bool {
        (blockers + warnings).contains { $0.range(of: "reload", options: .caseInsensitive) != nil }
    }

    /// Apply is offered: something changes and nothing blocks.
    public var canApply: Bool { !noChange && blockers.isEmpty && !(labels.isEmpty && taints.isEmpty) }
}
