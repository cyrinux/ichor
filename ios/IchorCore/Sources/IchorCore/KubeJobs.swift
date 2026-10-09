import Foundation

/// The Jobs screen (Go `KubeJobs`): each Job, failures first, then suspended and running ones.
public struct KubeJobs: Decodable, Sendable {
    public let jobs: [JobRow]

    private enum CodingKeys: String, CodingKey { case jobs }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        jobs = try c.field(.jobs, [])
    }
}

/// A Job. `state`: running, succeeded, failed or suspended. `started` and `finished` in unix
/// ms (`finished` 0 while running); `duration` in ms, up to the read while it runs.
/// `completions`: kubectl's "1/1". `owner`: the CronJob that created it, "" for none.
public struct JobRow: Decodable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let state: String
    public let owner: String
    public let manual: Bool
    public let started: Int64
    public let finished: Int64
    public let duration: Int64
    public let completions: String
    /// Why a failed Job stopped (BackoffLimitExceeded, DeadlineExceeded...).
    public let reason: String
    /// The screens share ok, warning and critical.
    public let level: StorageLevel

    public var id: String { "\(namespace)/\(name)" }
    /// Held by spec.suspend: it runs no pod until resumed.
    public var suspended: Bool { state == "suspended" }
    /// The run state the CronJobs screen also shows; nil for a suspended Job.
    public var runState: JobRunState? { suspended ? nil : JobRunState(rawValue: state) ?? .never }
    /// How long it ran, in seconds; nil before it started.
    public var durationSeconds: Int64? { started > 0 && duration > 0 ? duration / 1000 : nil }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, state, owner, manual, started, finished, duration, completions, reason, level
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        state = try c.field(.state, "")
        owner = try c.field(.owner, "")
        manual = try c.field(.manual, false)
        started = try c.field(.started, 0)
        finished = try c.field(.finished, 0)
        duration = try c.field(.duration, 0)
        completions = try c.field(.completions, "")
        reason = try c.field(.reason, "")
        level = try c.wire(.level)
    }
}

/// The Jobs whose namespace/name, owner CronJob, state or reason contains `query`.
public func filterJobs(_ jobs: [JobRow], query: String) -> [JobRow] {
    let q = query.trimmingCharacters(in: .whitespaces)
    guard !q.isEmpty else { return jobs }
    return jobs.filter { j in
        [j.id, j.owner, j.state, j.reason].contains { $0.localizedCaseInsensitiveContains(q) }
    }
}
