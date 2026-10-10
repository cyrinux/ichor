import Foundation

// Mirrors go/ichorgo/prom_rules.go, prom_targets.go and prom_operator.go: the rule groups and
// scrape targets of the Prometheus the Metrics screen uses, and the prometheus-operator objects.

/// An object to open in the Kubernetes browser.
public struct KubeObjectLink: Hashable, Sendable {
    public let resource: KubeAPIResource
    public let namespace: String
    public let name: String

    public init(resource: KubeAPIResource, namespace: String, name: String) {
        self.resource = resource
        self.namespace = namespace
        self.name = name
    }
}

/// The prometheus-operator API group.
public let promOperatorGroup = "monitoring.coreos.com"

// MARK: - Rules

public enum PromRuleType: String, Sendable, WireEnum {
    case alerting, recording

    public static let wireFallback: Self = .recording
}

public enum PromRuleState: String, Sendable, WireEnum {
    case firing, pending, inactive

    public static let wireFallback: Self = .inactive
}

public enum PromRuleHealth: String, Sendable, WireEnum {
    case ok, err, unknown

    public static let wireFallback: Self = .unknown
}

/// One rule. `interval`-like fields in seconds, `lastEvaluation` in unix ms (0: never).
public struct PromRule: Decodable, Equatable, Sendable {
    public let name: String
    public let type: PromRuleType
    public let state: PromRuleState
    public let health: PromRuleHealth
    public let lastError: String
    public let query: String
    public let severity: String
    /// `for:`, in seconds.
    public let duration: Double
    /// Active alerts (pending or firing) of an alerting rule.
    public let alerts: Int
    public let lastEvaluation: Int64

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        type = try c.wire(.type)
        state = try c.wire(.state)
        health = try c.wire(.health)
        lastError = try c.field(.lastError, "")
        query = try c.field(.query, "")
        severity = try c.field(.severity, "")
        duration = try c.field(.duration, 0)
        alerts = try c.field(.alerts, 0)
        lastEvaluation = try c.field(.lastEvaluation, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, type, state, health, lastError, query, severity, duration, alerts, lastEvaluation }
}

/// A rule group, troubled first; `ruleNamespace`/`ruleName` name its PrometheusRule ("" when unknown).
public struct PromRuleGroup: Decodable, Equatable, Sendable, Identifiable {
    public let name: String
    public let file: String
    public let ruleNamespace: String
    public let ruleName: String
    public let interval: Double
    public let evaluationTime: Double
    public let lastEvaluation: Int64
    public let lastError: String
    public let firing: Int
    public let pending: Int
    public let errors: Int
    public let rules: [PromRule]

    public var id: String { "\(file)|\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        file = try c.field(.file, "")
        ruleNamespace = try c.field(.ruleNamespace, "")
        ruleName = try c.field(.ruleName, "")
        interval = try c.field(.interval, 0)
        evaluationTime = try c.field(.evaluationTime, 0)
        lastEvaluation = try c.field(.lastEvaluation, 0)
        lastError = try c.field(.lastError, "")
        firing = try c.field(.firing, 0)
        pending = try c.field(.pending, 0)
        errors = try c.field(.errors, 0)
        rules = try c.field(.rules, [])
    }

    private enum CodingKeys: String, CodingKey {
        case name, file, ruleNamespace, ruleName, interval, evaluationTime, lastEvaluation, lastError, firing, pending, errors, rules
    }

    public var inTrouble: Bool { errors > 0 || firing > 0 || pending > 0 }

    /// The PrometheusRule the group comes from, nil when Go could not tell.
    public var ruleLink: KubeObjectLink? {
        guard !ruleNamespace.isEmpty, !ruleName.isEmpty else { return nil }
        return KubeObjectLink(resource: KubeAPIResource(group: promOperatorGroup, resource: "prometheusrules", kind: "PrometheusRule"),
                              namespace: ruleNamespace, name: ruleName)
    }
}

public struct PromRuleCounts: Decodable, Equatable, Sendable {
    public let groups: Int
    public let rules: Int
    public let firing: Int
    public let pending: Int
    public let errors: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        groups = try c.field(.groups, 0)
        rules = try c.field(.rules, 0)
        firing = try c.field(.firing, 0)
        pending = try c.field(.pending, 0)
        errors = try c.field(.errors, 0)
    }

    public init(groups: Int = 0, rules: Int = 0, firing: Int = 0, pending: Int = 0, errors: Int = 0) {
        self.groups = groups
        self.rules = rules
        self.firing = firing
        self.pending = pending
        self.errors = errors
    }

    private enum CodingKeys: String, CodingKey { case groups, rules, firing, pending, errors }
}

/// The answer of PromRules; `counts` cover every rule even when `truncated`.
public struct PromRules: Decodable, Equatable, Sendable {
    public let groups: [PromRuleGroup]
    public let counts: PromRuleCounts
    public let truncated: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        groups = try c.field(.groups, [])
        counts = try c.field(.counts, PromRuleCounts())
        truncated = try c.field(.truncated, false)
    }

    private enum CodingKeys: String, CodingKey { case groups, counts, truncated }
}

// MARK: - Targets

/// A target that is down: what Prometheus scraped and why it failed.
public struct PromDownTarget: Decodable, Equatable, Sendable, Identifiable {
    public let scrapeUrl: String
    public let lastError: String
    /// Unix ms, 0 when never.
    public let lastScrape: Int64
    public let lastScrapeDuration: Double
    public let job: String
    public let namespace: String
    public let service: String
    public let pod: String
    public let instance: String

    public var id: String { scrapeUrl.isEmpty ? instance : scrapeUrl }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        scrapeUrl = try c.field(.scrapeUrl, "")
        lastError = try c.field(.lastError, "")
        lastScrape = try c.field(.lastScrape, 0)
        lastScrapeDuration = try c.field(.lastScrapeDuration, 0)
        job = try c.field(.job, "")
        namespace = try c.field(.namespace, "")
        service = try c.field(.service, "")
        pod = try c.field(.pod, "")
        instance = try c.field(.instance, "")
    }

    private enum CodingKeys: String, CodingKey { case scrapeUrl, lastError, lastScrape, lastScrapeDuration, job, namespace, service, pod, instance }

    /// The pod it scrapes, nil when the target has none (kubelet, API server).
    public var podLink: KubeObjectLink? {
        guard !namespace.isEmpty, !pod.isEmpty else { return nil }
        return KubeObjectLink(resource: KubeAPIResource(resource: "pods", kind: "Pod"), namespace: namespace, name: pod)
    }

    /// The Service it scrapes through, nil when unknown.
    public var serviceLink: KubeObjectLink? {
        guard !namespace.isEmpty, !service.isEmpty else { return nil }
        return KubeObjectLink(resource: KubeAPIResource(resource: "services", kind: "Service"), namespace: namespace, name: service)
    }

    /// Where a tap goes: the pod, else the Service, else (`pool`) the monitor.
    public func link(in pool: PromScrapePool) -> KubeObjectLink? { podLink ?? serviceLink ?? pool.monitorLink }
}

/// A scrape pool with its counts and its down targets; `kind` "" for a job of the configuration.
public struct PromScrapePool: Decodable, Equatable, Sendable, Identifiable {
    public let pool: String
    public let kind: String
    public let namespace: String
    public let name: String
    /// The index in the monitor's endpoints, -1 for a Probe, a ScrapeConfig or a job.
    public let endpoint: Int
    public let up: Int
    public let down: Int
    public let unknown: Int
    public let targets: [PromDownTarget]

    public var id: String { pool }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pool = try c.field(.pool, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        endpoint = try c.field(.endpoint, -1)
        up = try c.field(.up, 0)
        down = try c.field(.down, 0)
        unknown = try c.field(.unknown, 0)
        targets = try c.field(.targets, [])
    }

    private enum CodingKeys: String, CodingKey { case pool, kind, namespace, name, endpoint, up, down, unknown, targets }

    /// "ServiceMonitor demo/hello-ichor", or the job name for a pool of the configuration.
    public var label: String { kind.isEmpty || name.isEmpty ? pool : "\(kind) \(namespace)/\(name)" }

    /// The ServiceMonitor, PodMonitor, Probe or ScrapeConfig behind the pool; nil for a job.
    public var monitorLink: KubeObjectLink? {
        guard !namespace.isEmpty, !name.isEmpty else { return nil }
        let resource: KubeAPIResource
        switch kind {
        case "ServiceMonitor": resource = KubeAPIResource(group: promOperatorGroup, resource: "servicemonitors", kind: kind)
        case "PodMonitor": resource = KubeAPIResource(group: promOperatorGroup, resource: "podmonitors", kind: kind)
        case "Probe": resource = KubeAPIResource(group: promOperatorGroup, resource: "probes", kind: kind)
        case "ScrapeConfig": resource = KubeAPIResource(group: promOperatorGroup, version: "v1alpha1", resource: "scrapeconfigs", kind: kind)
        default: return nil
        }
        return KubeObjectLink(resource: resource, namespace: namespace, name: name)
    }
}

/// The answer of PromTargets: every active pool, most down first, and the totals.
public struct PromTargets: Decodable, Equatable, Sendable {
    public let pools: [PromScrapePool]
    public let up: Int
    public let down: Int
    public let unknown: Int
    public let total: Int
    public let truncated: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pools = try c.field(.pools, [])
        up = try c.field(.up, 0)
        down = try c.field(.down, 0)
        unknown = try c.field(.unknown, 0)
        total = try c.field(.total, 0)
        truncated = try c.field(.truncated, false)
    }

    private enum CodingKeys: String, CodingKey { case pools, up, down, unknown, total, truncated }

    /// The pools with a target down, the ones the screen lists.
    public var troubledPools: [PromScrapePool] { pools.filter { $0.down > 0 } }
}

// MARK: - Operator

public enum PromOperatorHealth: String, Sendable, WireEnum {
    case critical, warning, ok

    public static let wireFallback: Self = .ok
}

/// A condition of a Prometheus or Alertmanager object, as set.
public struct PromOperatorCondition: Decodable, Equatable, Sendable {
    public let type: String
    public let status: String
    public let reason: String
    public let message: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case type, status, reason, message }

    /// True for Available or Reconciled "True"; Degraded and False are trouble.
    public var healthy: Bool { status == "True" }
}

/// A Prometheus or Alertmanager object: replicas available of desired, conditions, health.
public struct PromOperatorServer: Decodable, Equatable, Sendable, Identifiable {
    public let namespace: String
    public let name: String
    public let version: String
    public let replicas: Int
    public let shards: Int
    public let desired: Int
    public let available: Int
    public let paused: Bool
    public let health: PromOperatorHealth
    public let conditions: [PromOperatorCondition]

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        version = try c.field(.version, "")
        replicas = try c.field(.replicas, 1)
        shards = try c.field(.shards, 1)
        desired = try c.field(.desired, 0)
        available = try c.field(.available, 0)
        paused = try c.field(.paused, false)
        health = try c.wire(.health)
        conditions = try c.field(.conditions, [])
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, version, replicas, shards, desired, available, paused, health, conditions }
}

/// The answer of PromOperatorStatus; not `installed` when the cluster has no monitoring.coreos.com.
public struct PromOperatorStatus: Decodable, Equatable, Sendable {
    public let installed: Bool
    public let error: String
    public let prometheuses: [PromOperatorServer]
    public let alertmanagers: [PromOperatorServer]
    public let serviceMonitors: Int
    public let podMonitors: Int
    public let prometheusRules: Int
    public let probes: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        error = try c.field(.error, "")
        prometheuses = try c.field(.prometheuses, [])
        alertmanagers = try c.field(.alertmanagers, [])
        serviceMonitors = try c.field(.serviceMonitors, 0)
        podMonitors = try c.field(.podMonitors, 0)
        prometheusRules = try c.field(.prometheusRules, 0)
        probes = try c.field(.probes, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case installed, error, prometheuses, alertmanagers, serviceMonitors, podMonitors, prometheusRules, probes
    }

    /// The worst health of its servers.
    public var health: PromOperatorHealth {
        let all = (prometheuses + alertmanagers).map(\.health)
        if all.contains(.critical) { return .critical }
        if all.contains(.warning) { return .warning }
        return .ok
    }

    /// Each server's object in the browser.
    public static func link(prometheus server: PromOperatorServer) -> KubeObjectLink {
        KubeObjectLink(resource: KubeAPIResource(group: promOperatorGroup, resource: "prometheuses", kind: "Prometheus"),
                       namespace: server.namespace, name: server.name)
    }

    public static func link(alertmanager server: PromOperatorServer) -> KubeObjectLink {
        KubeObjectLink(resource: KubeAPIResource(group: promOperatorGroup, resource: "alertmanagers", kind: "Alertmanager"),
                       namespace: server.namespace, name: server.name)
    }
}
