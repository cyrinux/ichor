import Foundation

// The alert rules (Settings > Alerts > Rules) as the Go core reads them (AlertRulesApply): a
// default rule and per-cluster overrides by context fingerprint. Stored, backed up and sent to
// Go as this JSON, the same as Android's model/AlertRules.kt: a field left out takes the
// default's, so nothing unset is written.

/// The minimum severities, lowest first, as the rules store them ("all" is stored as none).
public let alertSeverityAll = "all"
public let alertSeverityInfo = "info"
public let alertSeverityWarning = "warning"
public let alertSeverityCritical = "critical"
public let alertRuleSeverities = [alertSeverityAll, alertSeverityWarning, alertSeverityCritical]

/// A daily window, "HH:MM" (24 h) wall-clock times; it may cross midnight. `start == end` is no
/// window (how a cluster turns off the default's). `tz` "" is the phone's zone, filled in at
/// check time. `exceptCritical`: critical alerts (node down, etcd, unreachable) still notify.
public struct QuietHours: Codable, Equatable, Hashable, Sendable {
    public var start: String
    public var end: String
    public var tz: String
    public var exceptCritical: Bool

    public init(start: String = "", end: String = "", tz: String = "", exceptCritical: Bool = false) {
        self.start = start
        self.end = end
        self.tz = tz
        self.exceptCritical = exceptCritical
    }

    public var isOff: Bool { start == end }

    /// What turning quiet hours on starts from.
    public static let night = QuietHours(start: "23:00", end: "07:00", tz: "", exceptCritical: true)
    /// A cluster's "Off": `{}`, no window.
    public static let off = QuietHours()

    private enum CodingKeys: String, CodingKey { case start, end, tz, exceptCritical }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        start = try c.field(.start, "")
        end = try c.field(.end, "")
        tz = try c.field(.tz, "")
        exceptCritical = try c.field(.exceptCritical, false)
    }

    /// Like Android's (kotlinx without encodeDefaults): an empty value is left out.
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        if !start.isEmpty { try c.encode(start, forKey: .start) }
        if !end.isEmpty { try c.encode(end, forKey: .end) }
        if !tz.isEmpty { try c.encode(tz, forKey: .tz) }
        if exceptCritical { try c.encode(exceptCritical, forKey: .exceptCritical) }
    }
}

/// One rule; nil takes the default's. `minSeverity`: all, warning or critical.
public struct AlertRule: Codable, Equatable, Hashable, Sendable {
    public var quietHours: QuietHours?
    public var minSeverity: String?
    public var digest: Bool?

    public init(quietHours: QuietHours? = nil, minSeverity: String? = nil, digest: Bool? = nil) {
        self.quietHours = quietHours
        self.minSeverity = minSeverity
        self.digest = digest
    }

    public var isEmpty: Bool { quietHours == nil && minSeverity == nil && digest == nil }

    /// With a window without a zone in `zone`.
    public func withZone(_ zone: String) -> AlertRule {
        guard var quiet = quietHours, quiet.tz.trimmingCharacters(in: .whitespaces).isEmpty else { return self }
        quiet.tz = zone
        var out = self
        out.quietHours = quiet
        return out
    }
}

/// The rules: a default and the overrides by cluster fingerprint.
public struct AlertRules: Codable, Equatable, Hashable, Sendable {
    public var `default`: AlertRule
    public var clusters: [String: AlertRule]

    public init(default rule: AlertRule = AlertRule(), clusters: [String: AlertRule] = [:]) {
        self.default = rule
        self.clusters = clusters
    }

    public static let none = AlertRules()

    /// The override of `fingerprint`, or an empty one (every field as default).
    public func clusterRule(_ fingerprint: String) -> AlertRule { clusters[fingerprint] ?? AlertRule() }

    /// With `fingerprint`'s override replaced by `rule`; an empty one is dropped.
    public func withCluster(_ fingerprint: String, _ rule: AlertRule) -> AlertRules {
        var next = clusters
        next[fingerprint] = rule.isEmpty ? nil : rule
        return AlertRules(default: self.default, clusters: next)
    }

    /// With the default rule replaced by `rule`.
    public func withDefault(_ rule: AlertRule) -> AlertRules { AlertRules(default: rule, clusters: clusters) }

    /// Only the overrides of the clusters in `fingerprints`: a removed cluster's go with it.
    public func keeping(_ fingerprints: [String]) -> AlertRules {
        let kept = Set(fingerprints)
        return AlertRules(default: self.default, clusters: clusters.filter { kept.contains($0.key) })
    }

    /// Every window without a zone (the phone's, as the settings store it) given `zone`, the
    /// phone's IANA zone now: Go has no local zone of its own.
    public func withZone(_ zone: String) -> AlertRules {
        AlertRules(default: self.default.withZone(zone), clusters: clusters.mapValues { $0.withZone(zone) })
    }

    private enum CodingKeys: String, CodingKey { case `default`, clusters }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.default = try c.field(.default, AlertRule())
        clusters = try c.field(.clusters, [:])
    }

    /// `{}` with nothing set, like Android's.
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        if !self.default.isEmpty { try c.encode(self.default, forKey: .default) }
        if !clusters.isEmpty { try c.encode(clusters, forKey: .clusters) }
    }

    /// The stored form.
    public var json: String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return (try? encoder.encode(self)).flatMap { String(data: $0, encoding: .utf8) } ?? "{}"
    }

    /// The stored rules; none when absent or unreadable (an older or broken value notifies as before).
    public static func decode(_ data: Data?) -> AlertRules {
        guard let data, let rules = try? JSONDecoder().decode(AlertRules.self, from: data) else { return .none }
        return rules
    }

    /// The rules for a backup, cluster overrides only of `fingerprints`; nil when none is set.
    public func forBackup(fingerprints: [String]) -> AlertRules? {
        let kept = keeping(fingerprints)
        return kept == .none ? nil : kept
    }
}

/// One alert a check is about to post, as Go reads it; `cluster` is the context fingerprint.
public struct AlertInput: Codable, Equatable, Sendable {
    public let cluster: String
    public let key: String
    public let kind: String
    public let severity: String
    public let problem: Bool
    public let snoozed: Bool

    public init(
        cluster: String, key: String, kind: String, severity: String = "", problem: Bool, snoozed: Bool = false
    ) {
        self.cluster = cluster
        self.key = key
        self.kind = kind
        self.severity = severity
        self.problem = problem
        self.snoozed = snoozed
    }
}

public let alertActionNotify = "notify"
public let alertActionMute = "mute"
public let alertActionDigest = "digest"

public let alertReasonSnoozed = "snoozed"
public let alertReasonSeverity = "severity"
public let alertReasonQuiet = "quiet"
public let alertReasonDigest = "digest"

/// What to do with one alert; `until` (unix ms) the end of the quiet window that muted it, else 0.
public struct AlertDecision: Decodable, Equatable, Sendable {
    public let cluster: String
    public let key: String
    public let action: String
    public let reason: String
    public let until: Int64

    public init(cluster: String, key: String, action: String, reason: String = "", until: Int64 = 0) {
        self.cluster = cluster
        self.key = key
        self.action = action
        self.reason = reason
        self.until = until
    }

    private enum CodingKeys: String, CodingKey { case cluster, key, action, reason, until }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cluster = try c.decode(String.self, forKey: .cluster)
        key = try c.decode(String.self, forKey: .key)
        action = try c.decode(String.self, forKey: .action)
        reason = try c.field(.reason, "")
        until = try c.field(.until, 0)
    }
}

/// A digest cluster's alerts of the run: how many, how many critical and warnings, the first keys.
public struct AlertDigest: Decodable, Equatable, Sendable {
    public let cluster: String
    public let count: Int
    public let critical: Int
    public let warning: Int
    public let firstKeys: [String]

    public init(cluster: String, count: Int, critical: Int = 0, warning: Int = 0, firstKeys: [String] = []) {
        self.cluster = cluster
        self.count = count
        self.critical = critical
        self.warning = warning
        self.firstKeys = firstKeys
    }

    private enum CodingKeys: String, CodingKey { case cluster, count, critical, warning, firstKeys }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cluster = try c.decode(String.self, forKey: .cluster)
        count = try c.decode(Int.self, forKey: .count)
        critical = try c.field(.critical, 0)
        warning = try c.field(.warning, 0)
        firstKeys = try c.field(.firstKeys, [])
    }
}

/// AlertRulesApply's answer: one decision per alert in order, one digest per digest cluster.
public struct AlertDecisions: Decodable, Equatable, Sendable {
    public let decisions: [AlertDecision]
    public let digests: [AlertDigest]

    public init(decisions: [AlertDecision] = [], digests: [AlertDigest] = []) {
        self.decisions = decisions
        self.digests = digests
    }

    private enum CodingKeys: String, CodingKey { case decisions, digests }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        decisions = try c.field(.decisions, [])
        digests = try c.field(.digests, [])
    }
}

/// What Go is sent: the rules with the phone's `zone` filled in, and the alerts.
public func alertRulesRequest(rules: AlertRules, alerts: [AlertInput], zone: String) throws -> (
    rules: String, alerts: String
) {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    let rulesJSON = String(decoding: try encoder.encode(rules.withZone(zone)), as: UTF8.self)
    let alertsJSON = String(decoding: try encoder.encode(alerts), as: UTF8.self)
    return (rulesJSON, alertsJSON)
}

/// "HH:MM" as (hour, minute), 0:00 when it is not one.
public func quietClock(_ value: String) -> (hour: Int, minute: Int) {
    let parts = value.split(separator: ":", omittingEmptySubsequences: false)
    guard parts.count == 2, let hour = Int(parts[0]), (0...23).contains(hour), let minute = Int(parts[1]),
        (0...59).contains(minute)
    else { return (0, 0) }
    return (hour, minute)
}

/// "07:05", as the rules store it.
public func quietClockText(hour: Int, minute: Int) -> String {
    String(format: "%02d:%02d", hour, minute)
}
