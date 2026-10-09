import Foundation

// Mirrors go/ichorgo/audit.go: the action audit log, every change the app made to a cluster
// (or tried to), kept on this device; and the filters of the screen that shows it.

/// One recorded action.
public struct ActivityEntry: Decodable, Equatable, Sendable {
    /// Unix ms.
    public let at: Int64
    /// The talosconfig or kubeconfig context.
    public let cluster: String
    public let node: String
    public let namespace: String
    /// "Kind/name".
    public let object: String
    /// A stable key: reboot, scale, rollout-restart, argo-sync...
    public let action: String
    /// A summary of what the action was given, secrets redacted.
    public let params: String
    /// ok or failed.
    public let outcome: String
    public let error: String
    /// Done on the demo cluster.
    public let demo: Bool

    public init(
        at: Int64 = 0, cluster: String = "", node: String = "", namespace: String = "", object: String = "",
        action: String = "", params: String = "", outcome: String = "ok", error: String = "", demo: Bool = false
    ) {
        self.at = at
        self.cluster = cluster
        self.node = node
        self.namespace = namespace
        self.object = object
        self.action = action
        self.params = params
        self.outcome = outcome
        self.error = error
        self.demo = demo
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.field(.at, 0)
        cluster = try c.field(.cluster, "")
        node = try c.field(.node, "")
        namespace = try c.field(.namespace, "")
        object = try c.field(.object, "")
        action = try c.field(.action, "")
        params = try c.field(.params, "")
        outcome = try c.field(.outcome, "ok")
        error = try c.field(.error, "")
        demo = try c.field(.demo, false)
    }

    private enum CodingKeys: String, CodingKey {
        case at, cluster, node, namespace, object, action, params, outcome, error, demo
    }

    public var failed: Bool { outcome != "ok" }

    public var date: Date { Date(timeIntervalSince1970: TimeInterval(at) / 1000) }

    /// What the entry acted on: the node, the namespace and the object that are set, joined by "/".
    public var target: String { [node, namespace, object].filter { !$0.isEmpty }.joined(separator: "/") }

    /// "rollout-restart" as "Rollout restart": the keys are plain English words.
    public var actionLabel: String { Self.label(of: action) }

    public static func label(of action: String) -> String {
        let words = action.replacingOccurrences(of: "-", with: " ")
        return words.prefix(1).uppercased() + words.dropFirst()
    }
}

/// The filters of the activity screen: nil for "any".
public struct ActivityFilter: Equatable, Sendable {
    public var cluster: String?
    public var action: String?
    public var failedOnly: Bool

    public init(cluster: String? = nil, action: String? = nil, failedOnly: Bool = false) {
        self.cluster = cluster
        self.action = action
        self.failedOnly = failedOnly
    }

    public func matches(_ entry: ActivityEntry) -> Bool {
        (cluster == nil || entry.cluster == cluster)
            && (action == nil || entry.action == action)
            && (!failedOnly || entry.failed)
    }
}

extension Array where Element == ActivityEntry {
    /// The distinct non-empty values of `key`, in order of first appearance (newest first).
    public func distinctValues(_ key: (ActivityEntry) -> String) -> [String] {
        var seen = Set<String>()
        return map(key).filter { !$0.isEmpty && seen.insert($0).inserted }
    }
}
