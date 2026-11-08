import Foundation

// The fleet widget and the watch's fleet complication: what the app keeps of the fleet screen's
// ranking after each background check (the widget extension cannot call the Go core). Same
// rules as Android (monitor/FleetGlance.kt, widget/FleetWidgetView.kt, core-widget FleetTally.kt).

/// One cluster as the last check ranked it: `name` as shown (masked in screenshot mode), `tone`
/// when ranked, `checkedAt` unix ms (0: never), `finding` the top finding already worded in the
/// app's language ("Nodes: node-b unreachable"; empty without one).
public struct FleetGlanceRow: Codable, Equatable, Sendable {
    public let key: String
    public let name: String
    public let tone: FleetTone
    public let checkedAt: Int64
    public let finding: String

    public init(key: String, name: String, tone: FleetTone, checkedAt: Int64, finding: String = "") {
        self.key = key
        self.name = name
        self.tone = tone
        self.checkedAt = checkedAt
        self.finding = finding
    }

    /// When it was checked; nil when never.
    public var checkedDate: Date? { checkedAt > 0 ? Date(epochMillis: checkedAt) : nil }

    /// The tone at `now`: a check turned stale since it was ranked reads as unknown, as the Go core ranks it.
    public func tone(at now: Date) -> FleetTone {
        guard let date = checkedDate, now.timeIntervalSince(date) <= glanceStaleAfter else { return .unknown }
        return tone
    }

    private enum CodingKeys: String, CodingKey { case key, name, tone, checkedAt, finding }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        name = try c.field(.name, "")
        tone = FleetTone(wire: try c.field(.tone, FleetTone.unknown.rawValue))
        checkedAt = try c.field(.checkedAt, 0)
        finding = try c.field(.finding, "")
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(key, forKey: .key)
        try c.encode(name, forKey: .name)
        try c.encode(tone.rawValue, forKey: .tone)
        try c.encode(checkedAt, forKey: .checkedAt)
        try c.encode(finding, forKey: .finding)
    }
}

/// The ranking, worst first, with no node, issue map or endpoint: only what the widget draws.
public struct FleetGlance: Codable, Equatable, Sendable {
    public let rows: [FleetGlanceRow]

    public init(rows: [FleetGlanceRow]) { self.rows = rows }

    /// The glance of `summary`, each top finding worded by `finding` (the app's words, its
    /// subject left out where screenshot mode hides it).
    public init(summary: FleetSummary, finding: (FleetFinding) -> String) {
        rows = summary.clusters.map { row in
            FleetGlanceRow(
                key: row.key, name: row.name, tone: row.tone, checkedAt: row.checkedAt,
                finding: row.topFinding.map(finding) ?? "")
        }
    }

    /// The rows named as `clusters` name them now (by monitor key, a rename since the check);
    /// the others as they were.
    public func renamed(_ clusters: [WidgetCluster]) -> FleetGlance {
        let names = Dictionary(clusters.map { ($0.id, $0.name) }, uniquingKeysWith: { first, _ in first })
        return FleetGlance(
            rows: rows.map { row in
                FleetGlanceRow(
                    key: row.key, name: names[row.key] ?? row.name, tone: row.tone, checkedAt: row.checkedAt,
                    finding: row.finding)
            })
    }

    /// The count per tone at `now`, with the newest check's time (what the watch gets).
    public func tally(at now: Date) -> FleetTally {
        let tones = rows.map { $0.tone(at: now) }
        return FleetTally(
            bad: tones.filter { $0 == .bad }.count, warn: tones.filter { $0 == .warn }.count,
            unknown: tones.filter { $0 == .unknown }.count, ok: tones.filter { $0 == .ok }.count,
            total: rows.count, takenAt: rows.compactMap(\.checkedDate).max())
    }

    /// What the widget draws at `now` with at most `maxRows` rows; nil before the first ranked check.
    public func view(at now: Date, maxRows: Int = fleetWidgetMaxRows) -> FleetGlanceView? {
        guard !rows.isEmpty else { return nil }
        let tones = rows.map { $0.tone(at: now) }
        // FleetTone's cases are worst first; bad rows then warn ones, each in the core's order.
        let needing = [FleetTone.bad, .warn].flatMap { tone in rows.indices.filter { tones[$0] == tone } }
        let worst = FleetTone.allCases.first { tones.contains($0) } ?? .unknown
        return FleetGlanceView(
            attention: needing.count, total: rows.count, tone: worst,
            rows: needing.prefix(max(0, maxRows)).map { rows[$0] },
            unchecked: tones.filter { $0 == .unknown }.count,
            allStale: rows.allSatisfy { row in
                row.checkedDate.map { now.timeIntervalSince($0) > glanceStaleAfter } ?? true
            },
            oldestCheck: rows.compactMap(\.checkedDate).min())
    }
}

/// The most "name · finding" rows the fleet widget lists.
public let fleetWidgetMaxRows = 3

/// What the fleet widget draws: `attention` of `total` clusters need a look, `tone` the worst
/// (the count's colour), `rows` the first of those; `unchecked` the ones not checked lately.
/// `allStale` when no cluster was checked within the hour: dimmed, with `oldestCheck`'s age.
public struct FleetGlanceView: Equatable, Sendable {
    public let attention: Int
    public let total: Int
    public let tone: FleetTone
    public let rows: [FleetGlanceRow]
    public let unchecked: Int
    public let allStale: Bool
    public let oldestCheck: Date?
}

/// How many clusters are in each tone after the phone's last check: the watch's fleet
/// complication. `takenAt` is the newest check (nil: none yet); once stale, so is the tally.
/// Counts only: no cluster name crosses to the watch with it.
public struct FleetTally: Codable, Equatable, Sendable {
    public let bad: Int
    public let warn: Int
    public let unknown: Int
    public let ok: Int
    public let total: Int
    public let takenAt: Date?

    public init(bad: Int = 0, warn: Int = 0, unknown: Int = 0, ok: Int = 0, total: Int = 0, takenAt: Date? = nil) {
        self.bad = bad
        self.warn = warn
        self.unknown = unknown
        self.ok = ok
        self.total = total
        self.takenAt = takenAt
    }

    /// Down, critical or degraded clusters.
    public var needAttention: Int { bad + warn }

    public func isStale(now: Date) -> Bool {
        takenAt.map { now.timeIntervalSince($0) > glanceStaleAfter } ?? true
    }

    /// The worst non-empty count's tone (bad, warn, unknown, ok); unknown for no fleet or a stale check.
    public func tone(at now: Date) -> FleetTone {
        if total == 0 || isStale(now: now) { return .unknown }
        if bad > 0 { return .bad }
        if warn > 0 { return .warn }
        if unknown > 0 { return .unknown }
        return ok > 0 ? .ok : .unknown
    }
}

/// The fleet glance shared by the app and its widget extension (App Group).
public enum SharedFleet {
    public static let key = "fleet"

    /// The glance saved in `defaults`, nil when there is none (an older version) or it does not decode.
    public static func load(from defaults: UserDefaults?) -> FleetGlance? {
        defaults?.data(forKey: key).flatMap { try? JSONDecoder().decode(FleetGlance.self, from: $0) }
    }
}

/// What a tap on the fleet widget opens: the fleet screen, once unlocked.
public let fleetWidgetURL = URL(string: "ichor://fleet")!

/// Whether `url` is the fleet widget's link.
public func isFleetLink(_ url: URL) -> Bool {
    url.scheme == "ichor" && url.host == "fleet" && (url.path.isEmpty || url.path == "/") && url.query == nil
}
