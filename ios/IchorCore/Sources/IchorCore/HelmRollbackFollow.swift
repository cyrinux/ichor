import Foundation

// Mirrors go/ichorgo/kube_helm_rollback.go (the options) and kube_helm_rollback_follow.go
// (StartHelmRollbackFollow: the release's workloads followed after a rollback, like
// helm rollback --wait).

/// How to roll back (KubeHelmRollbackPlan and KubeHelmRollback's options JSON). `wait` stays in
/// the app: after the rollback it follows the workloads (StartHelmRollbackFollow).
public struct HelmRollbackOptions: Encodable, Equatable, Hashable, Sendable {
    /// Like helm rollback --force: each changed object is written whole (PUT), not patched.
    public var force: Bool
    /// Like helm rollback --history-max: revisions kept after the rollback, 0 keeps them all.
    public var historyMax: Int
    public var wait: Bool

    /// The Keep the last revisions stepper.
    public static let historyMaxRange = 0...50

    public init(force: Bool = false, historyMax: Int = 10, wait: Bool = true) {
        self.force = force
        self.historyMax = historyMax
        self.wait = wait
    }

    private enum CodingKeys: String, CodingKey { case force, historyMax }

    /// The options JSON the Go calls take (`wait` is not sent).
    public func json() throws -> String { try TalosJSON.encode(self) }

    /// What the plan depends on (the dry run differs): `wait` does not change it.
    public var planKey: String { "\(force)/\(historyMax)" }
}

/// Phases of a followed rollback wait (HelmRollbackListener.OnProgress and OnDone's result).
public enum HelmRollbackWaitPhase {
    public static let running = "Running"
    public static let succeeded = "Succeeded"
    public static let failed = "Failed"
    /// Still rolling out after 5 minutes (Helm's default --timeout).
    public static let timeout = "Timeout"
}

/// A Deployment, StatefulSet or DaemonSet of the release, as the wait last read it.
public struct HelmRollbackWorkload: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    /// Pods of the rolled-back template that are ready, of `desired`.
    public let ready: Int
    public let desired: Int
    public let done: Bool
    /// Its progress deadline was exceeded.
    public let failed: Bool
    /// OnDelete or a StatefulSet partition: pods are replaced only when deleted (counted done).
    public let manual: Bool

    public var id: String { "\(kind)/\(namespace)/\(name)" }

    public init(
        kind: String, namespace: String = "", name: String, ready: Int = 0, desired: Int = 0,
        done: Bool = false, failed: Bool = false, manual: Bool = false
    ) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.ready = ready
        self.desired = desired
        self.done = done
        self.failed = failed
        self.manual = manual
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, ready, desired, done, failed, manual }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        ready = try c.field(.ready, 0)
        desired = try c.field(.desired, 0)
        done = try c.field(.done, false)
        failed = try c.field(.failed, false)
        manual = try c.field(.manual, false)
    }

    /// "Deployment web" (the namespace is the release's).
    public var label: String { "\(kind) \(name)" }

    /// The row's bar, 0…1: ready of desired, full once done (a workload scaled to 0 too).
    public var fraction: Double {
        if done || manual { return 1 }
        guard desired > 0 else { return 0 }
        return min(Double(max(ready, 0)) / Double(desired), 1)
    }
}

/// Where a rollback wait stands (HelmRollbackListener.OnProgress).
public struct HelmRollbackFollowProgress: Decodable, Equatable, Sendable {
    public let phase: String
    /// Workloads done (manual ones included), of `total`.
    public let done: Int
    public let total: Int
    public let workloads: [HelmRollbackWorkload]
    /// Unix ms.
    public let at: Int64

    public init(phase: String, done: Int = 0, total: Int = 0, workloads: [HelmRollbackWorkload] = [], at: Int64 = 0) {
        self.phase = phase
        self.done = done
        self.total = total
        self.workloads = workloads
        self.at = at
    }

    private enum CodingKeys: String, CodingKey { case phase, done, total, workloads, at }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
        workloads = try c.field(.workloads, [])
        at = try c.field(.at, 0)
    }

    /// The first workload whose rollout failed: "Deployment web did not become ready".
    public var failedWorkload: HelmRollbackWorkload? { workloads.first { $0.failed } }

    /// Some workload replaces its pods only when they are deleted.
    public var hasManual: Bool { workloads.contains { $0.manual } }
}

extension FollowOutcome {
    /// The rollback wait's end (HelmRollbackListener.OnDone), as `flux`: Timeout is still running.
    public static func helmWait(result: String, error: String?) -> FollowOutcome {
        if let error, !error.isEmpty { return .failed(error) }
        switch result {
        case "": return .unfollowed
        case HelmRollbackWaitPhase.succeeded: return .succeeded
        case HelmRollbackWaitPhase.timeout: return .stillRunning
        default: return .failed("")
        }
    }
}

/// Whether a rollback error only says the old revisions could not be deleted ("history not
/// pruned: …"): the rollback itself is done and recorded, as on Android.
public func isHelmHistoryNotPruned(_ message: String) -> Bool {
    message.contains("history not pruned:")
}
