import Foundation

// Mirrors go/ichorgo/alertmanager*.go: the alerts an Alertmanager holds, its silences, and
// silencing or expiring from the phone. Reached like a metrics source (the same PromSource,
// kind "alertmanager" or "mimir"), kept per cluster apart from it.

/// An alert's severity, folded by Go from its `severity` label (critical/crit/page… → critical).
public enum AMSeverity: String, CaseIterable, Sendable, WireEnum {
    case critical, warning, info, other

    public static let wireFallback = AMSeverity.other

    /// Worst first, for sorting.
    public var rank: Int {
        switch self {
        case .critical: 0
        case .warning: 1
        case .info: 2
        case .other: 3
        }
    }
}

/// `active` also covers `unprocessed`; `suppressed` is silenced or inhibited.
public enum AMAlertState: String, Sendable, WireEnum {
    case active, suppressed, unprocessed

    public static let wireFallback = AMAlertState.active
}

/// Alertmanager's own matcher: `=` (isEqual), `!=`, `=~` (isRegex) or `!~`.
public struct AMMatcher: Codable, Hashable, Sendable, Identifiable {
    public var name: String
    public var value: String
    public var isRegex: Bool
    public var isEqual: Bool

    public init(name: String, value: String, isRegex: Bool = false, isEqual: Bool = true) {
        self.name = name
        self.value = value
        self.isRegex = isRegex
        self.isEqual = isEqual
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(name: try c.field(.name, ""), value: try c.field(.value, ""),
                  isRegex: try c.field(.isRegex, false), isEqual: try c.field(.isEqual, true))
    }

    private enum CodingKeys: String, CodingKey { case name, value, isRegex, isEqual }

    public var id: String { "\(name)\(op)\(value)" }

    /// "=", "!=", "=~" or "!~".
    public var op: String {
        switch (isRegex, isEqual) {
        case (false, true): "="
        case (false, false): "!="
        case (true, true): "=~"
        case (true, false): "!~"
        }
    }

    /// `alertname="Watchdog"`, as Alertmanager writes it.
    public var text: String { "\(name)\(op)\"\(value)\"" }
}

public struct AMAlert: Decodable, Hashable, Identifiable, Sendable {
    public let fingerprint: String
    public let alertname: String
    public let severity: AMSeverity
    public let labels: [String: String]
    public let annotations: [String: String]
    public let summary: String
    public let description: String
    public let runbookURL: String
    public let generatorURL: String
    /// Unix ms, 0 when unset.
    public let startsAt: Int64
    public let endsAt: Int64
    public let updatedAt: Int64
    public let receivers: [String]
    public let state: AMAlertState
    public let silencedBy: [String]
    public let inhibitedBy: [String]

    public init(fingerprint: String = "", alertname: String = "", severity: AMSeverity = .other, labels: [String: String] = [:],
                annotations: [String: String] = [:], summary: String = "", description: String = "", runbookURL: String = "",
                generatorURL: String = "", startsAt: Int64 = 0, endsAt: Int64 = 0, updatedAt: Int64 = 0, receivers: [String] = [],
                state: AMAlertState = .active, silencedBy: [String] = [], inhibitedBy: [String] = []) {
        self.fingerprint = fingerprint
        self.alertname = alertname
        self.severity = severity
        self.labels = labels
        self.annotations = annotations
        self.summary = summary
        self.description = description
        self.runbookURL = runbookURL
        self.generatorURL = generatorURL
        self.startsAt = startsAt
        self.endsAt = endsAt
        self.updatedAt = updatedAt
        self.receivers = receivers
        self.state = state
        self.silencedBy = silencedBy
        self.inhibitedBy = inhibitedBy
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(fingerprint: try c.field(.fingerprint, ""), alertname: try c.field(.alertname, ""), severity: try c.wire(.severity),
                  labels: try c.field(.labels, [:]), annotations: try c.field(.annotations, [:]), summary: try c.field(.summary, ""),
                  description: try c.field(.description, ""), runbookURL: try c.field(.runbookURL, ""),
                  generatorURL: try c.field(.generatorURL, ""), startsAt: try c.field(.startsAt, 0), endsAt: try c.field(.endsAt, 0),
                  updatedAt: try c.field(.updatedAt, 0), receivers: try c.field(.receivers, []), state: try c.wire(.state),
                  silencedBy: try c.field(.silencedBy, []), inhibitedBy: try c.field(.inhibitedBy, []))
    }

    private enum CodingKeys: String, CodingKey {
        case fingerprint, alertname, severity, labels, annotations, summary, description, runbookURL, generatorURL
        case startsAt, endsAt, updatedAt, receivers, state, silencedBy, inhibitedBy
    }

    public var id: String { fingerprint }
    public var suppressed: Bool { state == .suppressed }
    public var silenced: Bool { !silencedBy.isEmpty }
    public var inhibited: Bool { !inhibitedBy.isEmpty }

    /// The annotations other than those shown on their own (summary, description, runbook).
    public var otherAnnotations: [(key: String, value: String)] {
        let shown: Set<String> = ["summary", "description", "message", "runbook_url", "runbook"]
        return annotations.filter { !shown.contains($0.key) }.sorted { $0.key < $1.key }.map { ($0.key, $0.value) }
    }

    /// What the alert is about, from its labels: "namespace/pod", the node, the instance, the
    /// service, the namespace; "" when none of them.
    public var subject: String {
        let ns = labels["namespace"] ?? ""
        if let pod = labels["pod"], !pod.isEmpty { return ns.isEmpty ? pod : "\(ns)/\(pod)" }
        for key in ["node", "instance", "service", "job"] {
            if let value = labels[key], !value.isEmpty { return value }
        }
        return ns
    }

    /// Whether `query` (lowercased, trimmed) is in its name, labels or summary.
    public func matches(_ query: String) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return true }
        if alertname.lowercased().contains(q) || summary.lowercased().contains(q) { return true }
        return labels.contains { $0.key.lowercased().contains(q) || $0.value.lowercased().contains(q) }
    }
}

public struct AMAlertGroup: Decodable, Hashable, Identifiable, Sendable {
    public let alertname: String
    /// The worst in the group.
    public let severity: AMSeverity
    public let count: Int
    /// Of which not suppressed.
    public let active: Int
    public let alerts: [AMAlert]

    public init(alertname: String = "", severity: AMSeverity = .other, count: Int = 0, active: Int = 0, alerts: [AMAlert] = []) {
        self.alertname = alertname
        self.severity = severity
        self.count = count
        self.active = active
        self.alerts = alerts
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(alertname: try c.field(.alertname, ""), severity: try c.wire(.severity), count: try c.field(.count, 0),
                  active: try c.field(.active, 0), alerts: try c.field(.alerts, []))
    }

    private enum CodingKeys: String, CodingKey { case alertname, severity, count, active, alerts }

    public var id: String { alertname }
}

/// Non-suppressed alerts by severity, plus the suppressed ones (the overview card).
public struct AMCounts: Decodable, Hashable, Sendable {
    public let critical: Int
    public let warning: Int
    public let info: Int
    public let other: Int
    public let suppressed: Int

    public init(critical: Int = 0, warning: Int = 0, info: Int = 0, other: Int = 0, suppressed: Int = 0) {
        self.critical = critical
        self.warning = warning
        self.info = info
        self.other = other
        self.suppressed = suppressed
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(critical: try c.field(.critical, 0), warning: try c.field(.warning, 0), info: try c.field(.info, 0),
                  other: try c.field(.other, 0), suppressed: try c.field(.suppressed, 0))
    }

    private enum CodingKeys: String, CodingKey { case critical, warning, info, other, suppressed }

    public func count(_ severity: AMSeverity) -> Int {
        switch severity {
        case .critical: critical
        case .warning: warning
        case .info: info
        case .other: other
        }
    }

    /// Alerts firing and not suppressed.
    public var firing: Int { critical + warning + info + other }
}

public struct AMAlerts: Decodable, Hashable, Sendable {
    public let groups: [AMAlertGroup]
    public let counts: AMCounts
    public let total: Int
    /// More than 1000 alerts: the groups hold the first 1000.
    public let truncated: Bool

    public init(groups: [AMAlertGroup] = [], counts: AMCounts = AMCounts(), total: Int = 0, truncated: Bool = false) {
        self.groups = groups
        self.counts = counts
        self.total = total
        self.truncated = truncated
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(groups: try c.field(.groups, []), counts: try c.field(.counts, AMCounts()), total: try c.field(.total, 0),
                  truncated: try c.field(.truncated, false))
    }

    private enum CodingKeys: String, CodingKey { case groups, counts, total, truncated }

    public var alerts: [AMAlert] { groups.flatMap(\.alerts) }

    /// The groups with only the alerts of `severities` matching `query`; groups left empty are dropped.
    public func filtered(severities: Set<AMSeverity>, query: String) -> [AMAlertGroup] {
        groups.compactMap { group in
            let kept = group.alerts.filter { severities.contains($0.severity) && $0.matches(query) }
            guard !kept.isEmpty else { return nil }
            let worst = kept.map(\.severity).min { $0.rank < $1.rank } ?? group.severity
            return AMAlertGroup(alertname: group.alertname, severity: worst, count: kept.count,
                                active: kept.filter { !$0.suppressed }.count, alerts: kept)
        }
    }
}

public enum AMSilenceState: String, Sendable, WireEnum {
    case active, pending, expired

    public static let wireFallback = AMSilenceState.expired
}

public struct AMSilence: Decodable, Hashable, Identifiable, Sendable {
    public let id: String
    public let state: AMSilenceState
    public let matchers: [AMMatcher]
    public let createdBy: String
    public let comment: String
    public let startsAt: Int64
    public let endsAt: Int64
    public let updatedAt: Int64

    public init(id: String = "", state: AMSilenceState = .active, matchers: [AMMatcher] = [], createdBy: String = "",
                comment: String = "", startsAt: Int64 = 0, endsAt: Int64 = 0, updatedAt: Int64 = 0) {
        self.id = id
        self.state = state
        self.matchers = matchers
        self.createdBy = createdBy
        self.comment = comment
        self.startsAt = startsAt
        self.endsAt = endsAt
        self.updatedAt = updatedAt
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(id: try c.field(.id, ""), state: try c.wire(.state), matchers: try c.field(.matchers, []),
                  createdBy: try c.field(.createdBy, ""), comment: try c.field(.comment, ""), startsAt: try c.field(.startsAt, 0),
                  endsAt: try c.field(.endsAt, 0), updatedAt: try c.field(.updatedAt, 0))
    }

    private enum CodingKeys: String, CodingKey { case id, state, matchers, createdBy, comment, startsAt, endsAt, updatedAt }

    /// Active or pending: it can still be expired.
    public var expirable: Bool { state != .expired }
}

public struct AMSilences: Decodable, Hashable, Sendable {
    public let silences: [AMSilence]

    public init(silences: [AMSilence] = []) { self.silences = silences }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        silences = try c.field(.silences, [])
    }

    private enum CodingKeys: String, CodingKey { case silences }
}

/// Which alert states the screen asks for (Alertmanager's own filters); all on by default.
public struct AMStateFilter: Hashable, Sendable {
    public var active: Bool
    public var silenced: Bool
    public var inhibited: Bool

    public init(active: Bool = true, silenced: Bool = true, inhibited: Bool = true) {
        self.active = active
        self.silenced = silenced
        self.inhibited = inhibited
    }
}

/// A cluster's Alertmanager setup; `source` nil until one is found or chosen.
public struct AlertmanagerConfig: Codable, Equatable, Sendable {
    public var source: PromSource?

    public init(source: PromSource? = nil) { self.source = source }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        source = try c.decodeIfPresent(PromSource.self, forKey: .source)
    }

    private enum CodingKeys: String, CodingKey { case source }
}

/// The silence durations offered, in minutes: 1h, 4h, 1d, 1w. Go takes 1 minute to 30 days.
public let amSilenceDurations = [60, 240, 1440, 10_080]
public let amMaxSilenceMinutes = 43_200

/// A custom duration typed as "90m", "6h", "2d", "1w" (or plain minutes) in minutes; nil when it
/// does not read or is out of Go's range.
public func amParseDuration(_ text: String) -> Int? {
    let t = text.trimmingCharacters(in: .whitespaces).lowercased()
    guard let last = t.last else { return nil }
    let unit: Int
    let digits: Substring
    switch last {
    case "m": (unit, digits) = (1, t.dropLast())
    case "h": (unit, digits) = (60, t.dropLast())
    case "d": (unit, digits) = (1440, t.dropLast())
    case "w": (unit, digits) = (10_080, t.dropLast())
    default: (unit, digits) = (1, Substring(t))
    }
    guard let n = Int(digits.trimmingCharacters(in: .whitespaces)), n > 0, n <= amMaxSilenceMinutes / unit else { return nil }
    return n * unit
}

/// The node an alert is about, among `nodes`: its `node` label, else the host of its `instance`
/// label ("10.0.0.5:9100", "[fd00::5]:9100", "cp-1:9100"), matched on hostname or address.
public func alertNode(_ labels: [String: String], in nodes: [NodeOverview]) -> NodeOverview? {
    var names: [String] = []
    if let node = labels["node"], !node.isEmpty { names.append(node) }
    if let instance = labels["instance"], !instance.isEmpty { names.append(amHost(instance)) }
    for name in names {
        if let found = nodes.first(where: { $0.hostname == name || $0.node == name }) { return found }
    }
    return nil
}

/// "host:port" → "host", "[v6]:port" → "v6"; a bare IPv6 address stays as it is.
func amHost(_ instance: String) -> String {
    if instance.hasPrefix("["), let close = instance.firstIndex(of: "]") {
        return String(instance[instance.index(after: instance.startIndex)..<close])
    }
    let colons = instance.filter { $0 == ":" }.count
    guard colons == 1, let colon = instance.lastIndex(of: ":") else { return instance }
    return String(instance[..<colon])
}
