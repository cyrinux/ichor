import Foundation

// Mirrors go/ichorgo/kube_apihealth.go: the Kubernetes API server's health and what puts
// pressure on it, and the pure logic the API server screen words it with.

/// The overall verdict, worst first.
public enum ApiStatus: String, Sendable, WireEnum {
    case unhealthy, throttling, busy, ok

    public static let wireFallback: Self = .ok
}

/// The API server's health and load. Rates are per second over `windowSeconds`, or since the
/// server started when it is 0 (two servers answered the scrapes).
public struct ApiHealthReport: Decodable, Equatable, Sendable {
    public let status: ApiStatus
    public let version: String
    public let ready: ApiProbe
    public let live: ApiProbe
    public let uptimeSeconds: Int64
    public let windowSeconds: Double
    public let requestRate: Double
    /// 5xx answers.
    public let errorRate: Double
    /// 429 answers.
    public let throttledRate: Double
    /// Refused by API Priority and Fairness.
    public let rejectedRate: Double
    public let inflightRead: Int
    public let inflightMutate: Int
    public let queued: Int
    public let watches: Int
    public let watchEventRate: Double
    public let etcdLatencyMs: Double
    public let clients: [ApiFlow]
    public let priorities: [ApiPriority]
    public let requests: [ApiRequestRow]
    public let watchedKinds: [ApiCount]
    public let objects: [ApiCount]
    public let queuedRequests: [ApiQueued]
    /// /metrics could not be read: only the probes are known.
    public let metricsError: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        status = try c.wire(.status)
        version = try c.field(.version, "")
        ready = try c.field(.ready, ApiProbe())
        live = try c.field(.live, ApiProbe())
        uptimeSeconds = try c.field(.uptimeSeconds, 0)
        windowSeconds = try c.field(.windowSeconds, 0)
        requestRate = try c.field(.requestRate, 0)
        errorRate = try c.field(.errorRate, 0)
        throttledRate = try c.field(.throttledRate, 0)
        rejectedRate = try c.field(.rejectedRate, 0)
        inflightRead = try c.field(.inflightRead, 0)
        inflightMutate = try c.field(.inflightMutate, 0)
        queued = try c.field(.queued, 0)
        watches = try c.field(.watches, 0)
        watchEventRate = try c.field(.watchEventRate, 0)
        etcdLatencyMs = try c.field(.etcdLatencyMs, 0)
        clients = try c.field(.clients, [])
        priorities = try c.field(.priorities, [])
        requests = try c.field(.requests, [])
        watchedKinds = try c.field(.watchedKinds, [])
        objects = try c.field(.objects, [])
        queuedRequests = try c.field(.queuedRequests, [])
        metricsError = try c.field(.metricsError, "")
    }

    private enum CodingKeys: String, CodingKey {
        case status, version, ready, live, uptimeSeconds, windowSeconds, requestRate, errorRate, throttledRate,
             rejectedRate, inflightRead, inflightMutate, queued, watches, watchEventRate, etcdLatencyMs, clients,
             priorities, requests, watchedKinds, objects, queuedRequests, metricsError
    }

    /// The checks that failed, readyz first, each once.
    public var failedChecks: [ApiCheck] {
        var seen = Set<String>()
        return (ready.checks + live.checks).filter { !$0.ok && seen.insert($0.name).inserted }
    }

    /// Whether the rates cover the last seconds, not the server's whole life.
    public var ratesAreLive: Bool { windowSeconds > 0 }
}

/// A /readyz or /livez answer; `error` when the probe itself got none.
public struct ApiProbe: Decodable, Equatable, Sendable {
    public let ok: Bool
    public let checks: [ApiCheck]
    public let error: String

    public init(ok: Bool = false, checks: [ApiCheck] = [], error: String = "") {
        self.ok = ok
        self.checks = checks
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.field(.ok, false)
        checks = try c.field(.checks, [])
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case ok, checks, error }
}

public struct ApiCheck: Decodable, Equatable, Sendable, Identifiable {
    public let name: String
    public let ok: Bool
    public let reason: String

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        ok = try c.field(.ok, false)
        reason = try c.field(.reason, "")
    }

    private enum CodingKeys: String, CodingKey { case name, ok, reason }
}

/// A flow schema: API Priority and Fairness's group of clients (nodes, controllers, service accounts…).
public struct ApiFlow: Decodable, Equatable, Sendable, Identifiable {
    public let name: String
    public let priority: String
    public let rate: Double
    public let rejectedRate: Double
    public let queued: Int
    /// Mean time queued before running.
    public let waitMs: Double

    /// A flow schema moved to another level keeps its old counters: name and level identify it.
    public var id: String { "\(name)/\(priority)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        priority = try c.field(.priority, "")
        rate = try c.field(.rate, 0)
        rejectedRate = try c.field(.rejectedRate, 0)
        queued = try c.field(.queued, 0)
        waitMs = try c.field(.waitMs, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, priority, rate, rejectedRate, queued, waitMs }
}

/// A priority level: the seats its requests use out of its `limit` (0 for exempt).
public struct ApiPriority: Decodable, Equatable, Sendable, Identifiable {
    public let name: String
    public let executing: Double
    public let limit: Double
    public let queued: Int
    public let rejectedRate: Double

    public var id: String { name }

    /// The share of the seats in use, nil for exempt (no limit).
    public var share: Double? { limit > 0 ? min(max(executing / limit, 0), 1) : nil }

    /// Past this share of its seats a level counts as nearly full, as in Go.
    public static let full = 0.8

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        executing = try c.field(.executing, 0)
        limit = try c.field(.limit, 0)
        queued = try c.field(.queued, 0)
        rejectedRate = try c.field(.rejectedRate, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, executing, limit, queued, rejectedRate }
}

/// One verb on one resource; an empty `resource` is a non-resource path such as /healthz.
public struct ApiRequestRow: Decodable, Equatable, Sendable, Identifiable {
    public let verb: String
    public let resource: String
    public let rate: Double
    public let errorRate: Double
    public let latencyMs: Double

    public var id: String { "\(verb) \(resource)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        verb = try c.field(.verb, "")
        resource = try c.field(.resource, "")
        rate = try c.field(.rate, 0)
        errorRate = try c.field(.errorRate, 0)
        latencyMs = try c.field(.latencyMs, 0)
    }

    private enum CodingKeys: String, CodingKey { case verb, resource, rate, errorRate, latencyMs }
}

public struct ApiCount: Decodable, Equatable, Sendable, Identifiable {
    public let resource: String
    public let count: Int

    public var id: String { resource }
}

/// A request waiting in an API Priority and Fairness queue now, with who sent it.
public struct ApiQueued: Decodable, Equatable, Sendable {
    public let user: String
    public let flowSchema: String
    public let priority: String
    public let verb: String
    public let path: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        user = try c.field(.user, "")
        flowSchema = try c.field(.flowSchema, "")
        priority = try c.field(.priority, "")
        verb = try c.field(.verb, "")
        path = try c.field(.path, "")
    }

    private enum CodingKeys: String, CodingKey { case user, flowSchema, priority, verb, path }
}

/// A rate as "142/s", "3.6/s" or "0.02/s".
public func formatRate(_ perSecond: Double) -> String {
    switch perSecond {
    case 10...: String(format: "%.0f/s", perSecond)
    case 1..<10: String(format: "%.1f/s", perSecond)
    case let r where r > 0: String(format: "%.2f/s", r)
    default: "0/s"
    }
}

/// A duration in milliseconds as "412 ms" or "6.8 ms".
public func formatMs(_ ms: Double) -> String {
    ms >= 10 ? String(format: "%.0f ms", ms) : String(format: "%.1f ms", ms)
}
