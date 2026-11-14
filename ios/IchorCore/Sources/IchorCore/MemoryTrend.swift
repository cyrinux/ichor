import Foundation

// Memory trend: the Go core's HistoryMemoryForecast (the storage forecast's line through each
// node's memory use over the last week), how a node's history words it, and the opt-in early
// alert ("memory:<node>:trend", with the storage trend's 3-day open, 7-day close hysteresis).

/// The memory use a node's projection counts to: where kubelet's memory pressure and evictions start.
public let memoryCriticalPercent = 90

/// Every node's memory projection (the Go core's HistoryMemoryForecast), sorted by node.
public struct HistoryMemoryForecast: Decodable, Equatable, Sendable {
    public let nodes: [NodeMemoryForecast]

    public init(nodes: [NodeMemoryForecast] = []) { self.nodes = nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
    }

    /// The row of the node at `address`; nil without one.
    public func node(_ address: String) -> NodeMemoryForecast? { nodes.first { $0.node == address } }

    private enum CodingKeys: String, CodingKey { case nodes }
}

/// One node: its latest memory use, its growth and, when the fit is good, the days until it is
/// critical and until it is full (counted from `latestAt`), as a VolumeForecast's.
public struct NodeMemoryForecast: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    public let usedPercent: Double
    public let latestAt: Int64
    public let slopePerDay: Double
    public let daysToFull: Double?
    public let daysToCritical: Double?
    public let confidence: String
    public let points: Int
    public let spanHours: Double

    public init(
        node: String, hostname: String = "", usedPercent: Double, latestAt: Int64 = 0, slopePerDay: Double = 0,
        daysToFull: Double? = nil, daysToCritical: Double? = nil, confidence: String = "low", points: Int = 0,
        spanHours: Double = 0
    ) {
        self.node = node
        self.hostname = hostname
        self.usedPercent = usedPercent
        self.latestAt = latestAt
        self.slopePerDay = slopePerDay
        self.daysToFull = daysToFull
        self.daysToCritical = daysToCritical
        self.confidence = confidence
        self.points = points
        self.spanHours = spanHours
    }

    private enum CodingKeys: String, CodingKey {
        case node, hostname, usedPercent, latestAt, slopePerDay, daysToFull, daysToCritical, confidence, points,
            spanHours
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        usedPercent = try c.field(.usedPercent, 0)
        latestAt = try c.field(.latestAt, 0)
        slopePerDay = try c.field(.slopePerDay, 0)
        daysToFull = try c.decodeIfPresent(Double.self, forKey: .daysToFull)
        daysToCritical = try c.decodeIfPresent(Double.self, forKey: .daysToCritical)
        confidence = try c.field(.confidence, "low")
        points = try c.field(.points, 0)
        spanHours = try c.field(.spanHours, 0)
    }

    /// The same row as a volume's, for the shared wording (VolumeForecastLine, StorageTrendIssue).
    var asVolume: VolumeForecast {
        VolumeForecast(
            key: node, name: memoryTrendName, node: node, usedPercent: usedPercent, latestAt: latestAt,
            slopePerDay: slopePerDay, daysToFull: daysToFull, daysToCritical: daysToCritical,
            confidence: confidence, points: points, spanHours: spanHours)
    }

    /// What the node's memory history says of it, worded like a volume row; nil for a flat node.
    public var line: VolumeForecastLine? { VolumeForecastLine(asVolume) }
}

/// The name a memory trend's StorageTrendIssue carries: its wording names the node only.
let memoryTrendName = "memory"

/// The alert key of a node's memory trend: "memory:<node address>:trend".
public func memoryTrendAlertKey(_ node: String) -> String { "memory:\(node):trend" }

/// The node address of a memory trend alert's subject ("<node>:trend"; an IPv6 address has colons too).
public func memoryTrendNode(subject: String) -> String {
    subject.hasSuffix(":trend") ? String(subject.dropLast(":trend".count)) : subject
}

/// English title of a memory trend alert: "worker-1 memory critical in ~3 days".
func memoryTrendTitle(_ issue: StorageTrendIssue) -> String {
    "\(issue.hostname) memory critical \(issue.when.english)"
}

/// English title of a memory trend alert resolved.
func memoryTrendClearedTitle(_ issue: StorageTrendIssue) -> String {
    "\(issue.hostname) memory no longer growing fast"
}

/// The memory trend alerts from `forecast`, with the storage trend's hysteresis on `open` (the
/// last run's open ones): a projected node critical within 3 days opens one at once, it stays open
/// while that stays within 7 days, and resolves once past 7 days or without a projection (a weak
/// fit, a reboot that restarted its series, the node gone). `hostnames` names the nodes by address.
public func evaluateMemoryTrends(
    _ forecast: HistoryMemoryForecast, open: [String: String], hostnames: [String: String]
) -> StorageTrendOutcome {
    var alerts: [Alert] = []
    var next: [String: String] = [:]
    for row in forecast.nodes where !row.node.isEmpty {
        guard row.confidence == VolumeForecast.high, let days = row.daysToCritical else { continue }
        let wasOpen = open[row.node] != nil
        guard days <= storageTrendOpenDays || (wasOpen && days <= storageTrendCloseDays) else { continue }
        let named = row.hostname.isEmpty ? hostnames[row.node] ?? "" : row.hostname
        let issue = StorageTrendIssue(row.asVolume, hostname: named.isEmpty ? row.node : named)
        next[row.node] = issue.value
        if !wasOpen {
            alerts.append(
                Alert(
                    key: memoryTrendAlertKey(row.node), title: memoryTrendTitle(issue),
                    text: storageTrendText(issue), problem: true))
        }
    }
    for node in open.keys.sorted() where next[node] == nil {
        let issue = StorageTrendIssue(value: open[node] ?? "")
        alerts.append(
            Alert(key: memoryTrendAlertKey(node), title: memoryTrendClearedTitle(issue), text: "", problem: false))
    }
    return StorageTrendOutcome(alerts: alerts, open: next)
}
