import Foundation

// What the Go core answers about a history ring (HistoryQuery, HistorySince), and the rules the
// screens apply to it: the uptime strip's segments, the "since you last looked" banner.

/// A time and a value (% used), as Go's [ms, value] pairs.
public struct HistoryPoint: Equatable, Sendable {
    public let at: Int64
    public let value: Double

    public init(at: Int64, value: Double) {
        self.at = at
        self.value = value
    }
}

private func points(_ pairs: [[Double]]) -> [HistoryPoint] {
    pairs.compactMap { pair in pair.count >= 2 ? HistoryPoint(at: Int64(pair[0]), value: pair[1]) : nil }
}

/// A stretch of time with no record (a gap).
public struct HistorySpan: Decodable, Equatable, Sendable {
    public let from: Int64
    public let to: Int64

    public init(from: Int64, to: Int64) {
        self.from = from
        self.to = to
    }
}

/// A node's state over a stretch: ready, notReady or unreachable.
public struct HistoryInterval: Decodable, Equatable, Sendable {
    public let state: String
    public let from: Int64
    public let to: Int64

    public init(state: String, from: Int64, to: Int64) {
        self.state = state
        self.from = from
        self.to = to
    }
}

public struct HistoryNodeUptime: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    /// Absent when the node was never observed in the window.
    public let uptimePercent: Double?
    public let intervals: [HistoryInterval]

    public init(node: String, hostname: String = "", uptimePercent: Double? = nil, intervals: [HistoryInterval] = []) {
        self.node = node
        self.hostname = hostname
        self.uptimePercent = uptimePercent
        self.intervals = intervals
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        uptimePercent = try c.decodeIfPresent(Double.self, forKey: .uptimePercent)
        intervals = try c.field(.intervals, [])
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, uptimePercent, intervals }
}

/// An alert from the time it opened to the time it closed (absent while open).
public struct HistoryAlertSpan: Decodable, Equatable, Sendable {
    public let key: String
    public let track: String
    public let severity: String?
    public let title: String?
    public let openedAt: Int64
    public let closedAt: Int64?

    public init(key: String, track: String, severity: String? = nil, title: String? = nil, openedAt: Int64, closedAt: Int64? = nil) {
        self.key = key
        self.track = track
        self.severity = severity
        self.title = title
        self.openedAt = openedAt
        self.closedAt = closedAt
    }

    /// What the screens name it by: its title, else its key.
    public var label: String { title.flatMap { $0.isEmpty ? nil : $0 } ?? key }

    /// As a record lists it (to resend it).
    public var record: HistoryAlertRecord { HistoryAlertRecord(key: key, track: track, severity: severity, title: title) }
}

public struct HistoryVolumeSeries: Decodable, Equatable, Sendable {
    public let key: String
    public let name: String
    public let node: String
    public let series: [HistoryPoint]

    public init(key: String, name: String, node: String, series: [HistoryPoint]) {
        self.key = key
        self.name = name
        self.node = node
        self.series = series
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        name = try c.field(.name, "")
        node = try c.field(.node, "")
        series = points(try c.field(.series, []))
    }

    private enum CodingKeys: String, CodingKey { case key, name, node, series }
}

public struct HistoryMemorySeries: Decodable, Equatable, Sendable {
    public let node: String
    public let series: [HistoryPoint]

    public init(node: String, series: [HistoryPoint]) {
        self.node = node
        self.series = series
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        series = points(try c.field(.series, []))
    }

    private enum CodingKeys: String, CodingKey { case node, series }
}

/// HistoryQuery's answer for a window.
public struct HistoryQueryResult: Decodable, Equatable, Sendable {
    public let from: Int64
    public let to: Int64
    public let records: Int
    /// Set after the ring could not be read and was started over.
    public let resetAt: Int64?
    public let resetReason: String?
    public let nodes: [HistoryNodeUptime]
    public let alerts: [HistoryAlertSpan]
    public let volumes: [HistoryVolumeSeries]
    public let memory: [HistoryMemorySeries]
    public let gaps: [HistorySpan]

    public init(from: Int64, to: Int64, records: Int = 0, nodes: [HistoryNodeUptime] = [], alerts: [HistoryAlertSpan] = [],
                volumes: [HistoryVolumeSeries] = [], memory: [HistoryMemorySeries] = [], gaps: [HistorySpan] = []) {
        self.from = from
        self.to = to
        self.records = records
        resetAt = nil
        resetReason = nil
        self.nodes = nodes
        self.alerts = alerts
        self.volumes = volumes
        self.memory = memory
        self.gaps = gaps
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.field(.from, 0)
        to = try c.field(.to, 0)
        records = try c.field(.records, 0)
        resetAt = try c.decodeIfPresent(Int64.self, forKey: .resetAt)
        resetReason = try c.decodeIfPresent(String.self, forKey: .resetReason)
        nodes = try c.field(.nodes, [])
        alerts = try c.field(.alerts, [])
        volumes = try c.field(.volumes, [])
        memory = try c.field(.memory, [])
        gaps = try c.field(.gaps, [])
    }

    private enum CodingKeys: String, CodingKey { case from, to, records, resetAt, resetReason, nodes, alerts, volumes, memory, gaps }

    /// The alerts still open: what a run resends for a track it could not read.
    public var openAlerts: [HistoryAlertRecord] { alerts.filter { $0.closedAt == nil }.map(\.record) }

    public func node(_ address: String) -> HistoryNodeUptime? { nodes.first { $0.node == address } }

    public func memory(of node: String) -> [HistoryPoint] { memory.first { $0.node == node }?.series ?? [] }

    /// The volume series of `node` by volume name.
    public func volumes(of node: String) -> [String: [HistoryPoint]] {
        Dictionary(volumes.filter { $0.node == node }.map { ($0.name, $0.series) }, uniquingKeysWith: { first, _ in first })
    }
}

// MARK: - Since you last looked

public struct HistoryNodeOutage: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    /// The worst seen: unreachable or notReady.
    public let state: String
    public let downAt: Int64
    /// Absent while still down.
    public let upAt: Int64?

    public init(node: String, hostname: String, state: String, downAt: Int64, upAt: Int64? = nil) {
        self.node = node
        self.hostname = hostname
        self.state = state
        self.downAt = downAt
        self.upAt = upAt
    }

    public var label: String { hostname.isEmpty ? node : hostname }
}

public struct HistoryUpgrade: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    public let from: String
    public let to: String
    public let at: Int64

    public init(node: String, hostname: String, from: String, to: String, at: Int64) {
        self.node = node
        self.hostname = hostname
        self.from = from
        self.to = to
        self.at = at
    }

    public var label: String { hostname.isEmpty ? node : hostname }
}

/// HistorySince's answer: what happened after `from` (the last look).
public struct HistorySinceSummary: Decodable, Equatable, Sendable {
    public let from: Int64
    public let to: Int64
    public let records: Int
    public let nodesRecovered: [HistoryNodeOutage]
    public let nodesDown: [HistoryNodeOutage]
    public let alertsResolved: [HistoryAlertSpan]
    public let alertsOpen: [HistoryAlertSpan]
    public let upgrades: [HistoryUpgrade]

    public init(from: Int64, to: Int64, records: Int = 0, nodesRecovered: [HistoryNodeOutage] = [], nodesDown: [HistoryNodeOutage] = [],
                alertsResolved: [HistoryAlertSpan] = [], alertsOpen: [HistoryAlertSpan] = [], upgrades: [HistoryUpgrade] = []) {
        self.from = from
        self.to = to
        self.records = records
        self.nodesRecovered = nodesRecovered
        self.nodesDown = nodesDown
        self.alertsResolved = alertsResolved
        self.alertsOpen = alertsOpen
        self.upgrades = upgrades
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.field(.from, 0)
        to = try c.field(.to, 0)
        records = try c.field(.records, 0)
        nodesRecovered = try c.field(.nodesRecovered, [])
        nodesDown = try c.field(.nodesDown, [])
        alertsResolved = try c.field(.alertsResolved, [])
        alertsOpen = try c.field(.alertsOpen, [])
        upgrades = try c.field(.upgrades, [])
    }

    private enum CodingKeys: String, CodingKey { case from, to, records, nodesRecovered, nodesDown, alertsResolved, alertsOpen, upgrades }

    /// Worth the banner: something ended or changed after the last look, or a node or an alert
    /// went bad after it. What was already down or open then was seen: alone, it shows nothing.
    public var hasNews: Bool {
        !nodesRecovered.isEmpty || !alertsResolved.isEmpty || !upgrades.isEmpty
            || nodesDown.contains { $0.downAt > from } || alertsOpen.contains { $0.openedAt > from }
    }

    /// An open alert that fired after the last look (the banner marks it new).
    public func isNew(_ alert: HistoryAlertSpan) -> Bool { alert.openedAt > from }
}

/// When the user last looked at a cluster's home (epoch ms), per cluster key.
public enum HistoryLastLooked {
    /// Seconds on the home before it counts as looked at.
    public static let dwellSeconds = 5

    /// The time to ask HistorySince from; nil the first time (nothing to compare with yet: the
    /// caller stores now and shows no banner).
    public static func since(stored: Int64?) -> Int64? {
        guard let stored, stored > 0 else { return nil }
        return stored
    }

    /// The stored value after a look at `now`: it never goes back.
    public static func advanced(stored: Int64?, to now: Int64) -> Int64 { max(stored ?? 0, now) }

    /// Only the clusters still stored keep theirs.
    public static func keep(_ map: [String: Int64], clusters: [String]) -> [String: Int64] {
        let kept = Set(clusters)
        return map.filter { kept.contains($0.key) }
    }
}

// MARK: - Uptime strip

/// The periods the uptime strip shows.
public enum HistoryPeriod: Int, CaseIterable, Sendable {
    case week = 7
    case month = 30

    public func since(now: Int64) -> Int64 { now - Int64(rawValue) * 86_400_000 }
}

/// A stretch of the strip: a node state, or no data.
public enum HistoryStripState: Equatable, Sendable {
    case ready, notReady, unreachable, noData

    init(state: String) {
        switch state {
        case "ready": self = .ready
        case "notReady": self = .notReady
        case "unreachable": self = .unreachable
        default: self = .noData
        }
    }
}

/// One segment of the strip, as fractions (0…1) of the window.
public struct HistoryStripSegment: Equatable, Sendable {
    public let state: HistoryStripState
    public let start: Double
    public let end: Double

    public init(state: HistoryStripState, start: Double, end: Double) {
        self.state = state
        self.start = start
        self.end = end
    }
}

/// `intervals` over [from, to] as contiguous segments, the time no interval covers as no data
/// (gaps, before the first record). Adjacent segments of one state are merged.
public func historyStrip(_ intervals: [HistoryInterval], from: Int64, to: Int64) -> [HistoryStripSegment] {
    guard to > from else { return [] }
    let length = Double(to - from)
    func fraction(_ t: Int64) -> Double { Double(min(max(t, from), to) - from) / length }
    var out: [HistoryStripSegment] = []
    func add(_ state: HistoryStripState, _ start: Int64, _ end: Int64) {
        let a = fraction(start)
        let b = fraction(end)
        guard b > a else { return }
        if let last = out.last, last.state == state, abs(last.end - a) < 1e-9 {
            out[out.count - 1] = HistoryStripSegment(state: state, start: last.start, end: b)
        } else {
            out.append(HistoryStripSegment(state: state, start: a, end: b))
        }
    }
    var cursor = from
    for interval in intervals.sorted(by: { $0.from < $1.from }) where interval.to > from && interval.from < to {
        let start = max(interval.from, cursor)
        if start > cursor { add(.noData, cursor, start) }
        let end = min(interval.to, to)
        if end > start { add(HistoryStripState(state: interval.state), start, end) }
        cursor = max(cursor, end)
    }
    if cursor < to { add(.noData, cursor, to) }
    return out
}

/// "97.9 %" (at most one decimal, in `locale`), "" when unknown.
public func historyUptimeText(_ percent: Double?, locale: Locale = .current) -> String {
    guard let percent else { return "" }
    let formatter = NumberFormatter()
    formatter.locale = locale
    formatter.minimumFractionDigits = 0
    formatter.maximumFractionDigits = 1
    return "\(formatter.string(from: NSNumber(value: percent)) ?? "") %"
}
