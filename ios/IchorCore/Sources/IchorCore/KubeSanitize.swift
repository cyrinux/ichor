import Foundation

// Clean up dead pods, like k9s's `z` (Go kube_sanitize.go): the preview lists the pods whose
// status k9s's sanitize deletes, the user unchecks some, and the sweep deletes exactly the rest.

/// The pods the sweep would delete (Go `KubeSanitizePreview`). `truncated`: the namespace has
/// more pods than one page, not read: narrow the namespace.
public struct KubeSanitizePreview: Decodable, Equatable, Sendable {
    public let pods: [SanitizePod]
    public let truncated: Bool

    public init(pods: [SanitizePod], truncated: Bool = false) {
        self.pods = pods
        self.truncated = truncated
    }

    private enum CodingKeys: String, CodingKey { case pods, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pods = try c.field(.pods, [])
        truncated = try c.field(.truncated, false)
    }
}

/// A pod the sweep would delete; `ownerKind`/`ownerName` recreate it ("" when none).
public struct SanitizePod: Decodable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let status: String
    public let ownerKind: String
    public let ownerName: String
    public let ageSec: Int64

    /// "namespace/name": what the sweep is passed.
    public var id: String { "\(namespace)/\(name)" }

    /// Its owner brings it back (a Deployment's ReplicaSet, a DaemonSet...); a Job's pod stays gone.
    public var hasOwner: Bool { !ownerKind.isEmpty && !ownerName.isEmpty }

    public init(
        namespace: String, name: String, status: String, ownerKind: String = "", ownerName: String = "",
        ageSec: Int64 = 0
    ) {
        self.namespace = namespace
        self.name = name
        self.status = status
        self.ownerKind = ownerKind
        self.ownerName = ownerName
        self.ageSec = ageSec
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, status, ownerKind, ownerName, ageSec }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        status = try c.field(.status, "")
        ownerKind = try c.field(.ownerKind, "")
        ownerName = try c.field(.ownerName, "")
        ageSec = try c.field(.ageSec, 0)
    }
}

/// What the sweep did (Go `StartKubeSanitize`): how many went, and each one that did not, with
/// why; `stopped`: ended by Stop before every pod was tried.
public struct KubeSanitizeResult: Decodable, Equatable, Sendable {
    public let deleted: Int
    public let failed: [SanitizeFailure]
    public let stopped: Bool

    public init(deleted: Int = 0, failed: [SanitizeFailure] = [], stopped: Bool = false) {
        self.deleted = deleted
        self.failed = failed
        self.stopped = stopped
    }

    private enum CodingKeys: String, CodingKey { case deleted, failed, stopped }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        deleted = try c.field(.deleted, 0)
        failed = try c.field(.failed, [])
        stopped = try c.field(.stopped, false)
    }
}

/// How far a running sweep is (Go `KubeSanitizeListener.OnProgress`): `done` pods tried of
/// `total`, `failed` of them not deleted; `name` the last one tried.
public struct PodSweepProgress: Decodable, Equatable, Sendable {
    public let done: Int
    public let total: Int
    public let name: String
    public let failed: Int

    public init(done: Int, total: Int, name: String = "", failed: Int = 0) {
        self.done = done
        self.total = total
        self.name = name
        self.failed = failed
    }

    private enum CodingKeys: String, CodingKey { case done, total, name, failed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
        name = try c.field(.name, "")
        failed = try c.field(.failed, 0)
    }
}

/// A "Clean up dead pods" sweep running in the background, or the last one: `done` pods tried
/// of `total` in `namespace` ("" for every namespace) of `context`, `deleted` of them gone;
/// `failures` names the pods it could not delete once it ended.
public struct PodSweepState: Equatable, Sendable {
    public let context: String
    public let namespace: String
    public private(set) var total: Int
    public private(set) var done = 0
    public private(set) var deleted = 0
    public private(set) var failedCount = 0
    public private(set) var failures: [SanitizeFailure] = []
    public private(set) var running = true
    /// Stop was asked: the core ends after the pod being deleted.
    public var stopping = false
    public private(set) var stopped = false
    public private(set) var error: String?

    public init(context: String, namespace: String, total: Int) {
        self.context = context
        self.namespace = namespace
        self.total = total
    }

    /// How far along the bar is, 0…1.
    public var fraction: Double { total > 0 ? Double(min(done, total)) / Double(total) : 0 }

    public mutating func apply(_ progress: PodSweepProgress) {
        done = progress.done
        if progress.total > 0 { total = progress.total }
        failedCount = progress.failed
        deleted = progress.done - progress.failed
    }

    /// The sweep ended with `result` (the pods tried so far); `error` when it was refused,
    /// could not run or ran out of time.
    public mutating func finish(_ result: KubeSanitizeResult, error: String?) {
        deleted = result.deleted
        failures = result.failed
        failedCount = result.failed.count
        done = result.deleted + result.failed.count
        stopped = result.stopped
        self.error = error
        running = false
    }
}

/// How many pods `sanitizeNames` lists: what a sweep of them counts to.
public func sanitizeCount(_ names: String) -> Int {
    names.split(separator: "\n").filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.count
}

/// A pod the sweep could not delete: "namespace/name" and the API server's error.
public struct SanitizeFailure: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    public let error: String

    public init(name: String, error: String) {
        self.name = name
        self.error = error
    }

    private enum CodingKeys: String, CodingKey { case name, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        error = try c.field(.error, "")
    }
}

/// The pods of one status, for the sheet's sections.
public struct SanitizeGroup: Equatable, Identifiable, Sendable {
    public let status: String
    public let pods: [SanitizePod]

    public var id: String { status }
}

/// `pods` grouped by status, the largest group first (then by status name), each group's pods
/// by namespace/name.
public func groupSanitizePods(_ pods: [SanitizePod]) -> [SanitizeGroup] {
    Dictionary(grouping: pods, by: \.status)
        .map { SanitizeGroup(status: $0.key, pods: $0.value.sorted { $0.id < $1.id }) }
        .sorted { $0.pods.count != $1.pods.count ? $0.pods.count > $1.pods.count : $0.status < $1.status }
}

/// A Pending pod younger than this may only be starting: left unchecked by default.
public let sanitizeFreshPendingSeconds: Int64 = 120

/// Checked by default: every pod but a Pending one younger than two minutes (still scheduling).
public func sanitizeCheckedByDefault(_ pod: SanitizePod) -> Bool {
    !(pod.status == "Pending" && pod.ageSec < sanitizeFreshPendingSeconds)
}

/// The ids of `pods` checked by default.
public func sanitizeDefaultSelection(_ pods: [SanitizePod]) -> Set<String> {
    Set(pods.filter(sanitizeCheckedByDefault).map(\.id))
}

/// The selection as the core takes it: newline-separated "namespace/name", in preview order.
public func sanitizeNames(_ pods: [SanitizePod], selected: Set<String>) -> String {
    pods.filter { selected.contains($0.id) }.map(\.id).joined(separator: "\n")
}
