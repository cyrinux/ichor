import Foundation

// Storage trend (CYR-128): the Go core's HistoryVolumeForecast (a line through each volume's fill
// over the last week), how a volume row words it, and the opt-in early alert that warns days
// before the fill alert opens ("storage:<key>:trend", same channel as the storage alerts).

/// Every volume's projection (the Go core's HistoryVolumeForecast), sorted by key.
public struct HistoryForecast: Decodable, Equatable, Sendable {
    public let volumes: [VolumeForecast]

    public init(volumes: [VolumeForecast] = []) { self.volumes = volumes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        volumes = try c.field(.volumes, [])
    }

    /// The row of the volume `name` on `node` (a user volume is also tried without its "u-"
    /// prefix, as the history names it); nil without one.
    public func volume(node: String, name: String) -> VolumeForecast? {
        let bare = name.hasPrefix("u-") ? String(name.dropFirst(2)) : name
        return volumes.first { $0.node == node && $0.name == name } ?? volumes.first { $0.node == node && $0.name == bare }
    }

    private enum CodingKeys: String, CodingKey { case volumes }
}

/// One volume: its latest fill, its growth and, when the fit is good, the days until it is
/// critical and until it is full (counted from `latestAt`).
public struct VolumeForecast: Decodable, Equatable, Sendable {
    public static let high = "high"

    /// "<node>|<volume>", the storage fill alert's key.
    public let key: String
    public let name: String
    public let node: String
    public let usedPercent: Double
    public let latestAt: Int64
    /// Percentage points a day.
    public let slopePerDay: Double
    public let daysToFull: Double?
    public let daysToCritical: Double?
    /// "high" (projected) or "low".
    public let confidence: String
    public let points: Int
    public let spanHours: Double

    public init(key: String, name: String, node: String, usedPercent: Double, latestAt: Int64 = 0, slopePerDay: Double = 0,
                daysToFull: Double? = nil, daysToCritical: Double? = nil, confidence: String = "low",
                points: Int = 0, spanHours: Double = 0) {
        self.key = key
        self.name = name
        self.node = node
        self.usedPercent = usedPercent
        self.latestAt = latestAt
        self.slopePerDay = slopePerDay
        self.daysToFull = daysToFull
        self.daysToCritical = daysToCritical
        self.confidence = confidence
        self.points = points
        self.spanHours = spanHours
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        name = try c.field(.name, "")
        node = try c.field(.node, "")
        usedPercent = try c.field(.usedPercent, 0)
        latestAt = try c.field(.latestAt, 0)
        slopePerDay = try c.field(.slopePerDay, 0)
        daysToFull = try c.decodeIfPresent(Double.self, forKey: .daysToFull)
        daysToCritical = try c.decodeIfPresent(Double.self, forKey: .daysToCritical)
        confidence = try c.field(.confidence, "low")
        points = try c.field(.points, 0)
        spanHours = try c.field(.spanHours, 0)
    }

    /// The fit is good enough to project.
    public var isHigh: Bool { confidence == Self.high }

    private enum CodingKeys: String, CodingKey {
        case key, name, node, usedPercent, latestAt, slopePerDay, daysToFull, daysToCritical, confidence, points, spanHours
    }
}

/// How far off a projection is, as a row or an alert words it.
public enum ForecastDays: Equatable, Sendable {
    /// Less than a day.
    case underADay
    /// About one day.
    case aboutADay
    /// About this many days (2 or more).
    case days(Int)

    /// `days` rounded: under 1 is "< 1 day".
    public init(_ days: Double) {
        if days < 1 {
            self = .underADay
            return
        }
        let rounded = Int(days.rounded())
        self = rounded <= 1 ? .aboutADay : .days(rounded)
    }

    /// English, after "full" or "critical": "in < 1 day", "in ~1 day", "in ~3 days".
    public var english: String {
        switch self {
        case .underADay: "in < 1 day"
        case .aboutADay: "in ~1 day"
        case .days(let n): "in ~\(n) days"
        }
    }
}

/// What a volume row says of its forecast: when it is full, when it is critical (only when that
/// comes before), or only its growth when the fit is too weak to project.
public enum VolumeForecastLine: Equatable, Sendable {
    case projected(full: ForecastDays?, critical: ForecastDays?)
    /// The growth (percentage points a day, as storagePercentText writes it), not projected.
    case growing(String)

    /// nil when there is nothing worth saying: a weak fit on a flat or shrinking volume, or a
    /// projection beyond a year.
    public init?(_ forecast: VolumeForecast) {
        if forecast.isHigh {
            let full = forecast.daysToFull.map(ForecastDays.init)
            // Critical only when it comes first: at a threshold of 100 % or rounded alike it says nothing more.
            var critical = forecast.daysToCritical.map(ForecastDays.init)
            if let fullDays = forecast.daysToFull, let criticalDays = forecast.daysToCritical,
               criticalDays >= fullDays || critical == full {
                critical = nil
            }
            guard full != nil || critical != nil else { return nil }
            self = .projected(full: full, critical: critical)
            return
        }
        let growth = storagePercentText(forecast.slopePerDay)
        guard forecast.slopePerDay > 0, growth != "0" else { return nil }
        self = .growing(growth)
    }
}

extension VolumeForecastLine {
    /// Projected critical or full within 7 days: worth standing out on the row.
    public var isWithinAWeek: Bool {
        guard case .projected(let full, let critical) = self else { return false }
        return [full, critical].compactMap { $0 }.contains { $0.rounded <= 7 }
    }
}

extension ForecastDays {
    /// Rounded days: 0 under a day.
    public var rounded: Int {
        switch self {
        case .underADay: 0
        case .aboutADay: 1
        case .days(let n): n
        }
    }
}

// MARK: - Early alert

/// The days to critical at which the trend alert opens, and past which it resolves: in between
/// it keeps its state, so a volume hovering around 3 days does not alert again and again.
public let storageTrendOpenDays = 3.0
public let storageTrendCloseDays = 7.0

/// An open trend alert's stored value, "hostname|name|critical|days|slope|used", split up: what
/// the notification names, kept so a resolved one can still be named once its row is gone.
public struct StorageTrendIssue: Equatable, Sendable {
    public let hostname: String
    public let name: String
    /// The alert names the critical threshold (it comes well before full), else full.
    public let critical: Bool
    /// Days until then.
    public let days: Double
    /// Percentage points a day.
    public let slopePerDay: Double
    public let usedPercent: Double

    public init(hostname: String, name: String, critical: Bool, days: Double, slopePerDay: Double, usedPercent: Double) {
        self.hostname = hostname
        self.name = name
        self.critical = critical
        self.days = days
        self.slopePerDay = slopePerDay
        self.usedPercent = usedPercent
    }

    /// From a projected row (`daysToCritical` set): critical when it rounds before full.
    public init(_ forecast: VolumeForecast, hostname: String) {
        let toCritical = forecast.daysToCritical ?? 0
        let toFull = forecast.daysToFull
        let critical = toFull.map { ForecastDays($0) != ForecastDays(toCritical) && toCritical < $0 } ?? true
        self.init(hostname: hostname, name: forecast.name, critical: critical, days: critical ? toCritical : toFull ?? toCritical,
                  slopePerDay: forecast.slopePerDay, usedPercent: forecast.usedPercent)
    }

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 5, omittingEmptySubsequences: false).map(String.init)
        func part(_ i: Int) -> String { parts.count > i ? parts[i] : "" }
        hostname = part(0)
        name = part(1)
        critical = part(2) == "critical"
        days = Double(part(3)) ?? 0
        slopePerDay = Double(part(4)) ?? 0
        usedPercent = Double(part(5)) ?? 0
    }

    /// The stored form; names lose any "|" so the parts still split.
    public var value: String {
        let safe = { (s: String) in s.replacingOccurrences(of: "|", with: "/") }
        return [safe(hostname), safe(name), critical ? "critical" : "full", String(days), String(slopePerDay), String(usedPercent)]
            .joined(separator: "|")
    }

    public var when: ForecastDays { ForecastDays(days) }
}

/// The alert key of a volume's trend: "storage:<node>|<volume>:trend".
public func storageTrendAlertKey(_ key: String) -> String { "storage:\(key):trend" }

/// The volume key of a trend alert's subject ("<node>|<volume>:trend"), nil for another storage alert.
public func storageTrendVolumeKey(subject: String) -> String? {
    subject.hasSuffix(":trend") ? String(subject.dropLast(":trend".count)) : nil
}

/// English title of a trend alert: "EPHEMERAL on worker-1 full in ~3 days".
func storageTrendTitle(_ issue: StorageTrendIssue) -> String {
    "\(issue.name) on \(issue.hostname) \(issue.critical ? "critical" : "full") \(issue.when.english)"
}

/// English text of a trend alert: "growing ~2 % a day, now at 56 %".
func storageTrendText(_ issue: StorageTrendIssue) -> String {
    "growing ~\(storagePercentText(issue.slopePerDay)) % a day, now at \(storagePercentText(issue.usedPercent)) %"
}

/// English title of a trend alert resolved.
func storageTrendClearedTitle(_ issue: StorageTrendIssue) -> String {
    "\(issue.name) on \(issue.hostname) no longer filling up fast"
}

/// The trend alerts of one run: those opened or resolved, and the ones open after it.
public struct StorageTrendOutcome: Equatable, Sendable {
    public let alerts: [Alert]
    /// Volume key → StorageTrendIssue value.
    public let open: [String: String]
}

/// The trend alerts from `forecast`, with hysteresis on `open` (the last run's open ones): a
/// projected volume critical within 3 days opens one at once (it is already a week's trend), it
/// stays open while that stays within 7 days, and resolves once past 7 days or without a
/// projection (a weak fit, the volume gone, or already critical). A volume in a fill alert
/// (`fillIssues`, at or above the warning threshold) opens none, and one open is dropped
/// silently: the fill alert speaks for it. `hostnames` names the nodes by address.
public func evaluateStorageTrends(_ forecast: HistoryForecast, open: [String: String], fillIssues: [String: String],
                                  hostnames: [String: String]) -> StorageTrendOutcome {
    var alerts: [Alert] = []
    var next: [String: String] = [:]
    for row in forecast.volumes where !row.key.isEmpty && fillIssues[row.key] == nil {
        guard row.isHigh, let days = row.daysToCritical else { continue }
        let wasOpen = open[row.key] != nil
        guard days <= storageTrendOpenDays || (wasOpen && days <= storageTrendCloseDays) else { continue }
        let host = hostnames[row.node].flatMap { $0.isEmpty ? nil : $0 } ?? row.node
        let issue = StorageTrendIssue(row, hostname: host)
        next[row.key] = issue.value
        if !wasOpen {
            alerts.append(Alert(key: storageTrendAlertKey(row.key), title: storageTrendTitle(issue), text: storageTrendText(issue),
                                problem: true))
        }
    }
    // Resolved: past 7 days, no projection, or the volume gone.
    for key in open.keys.sorted() where next[key] == nil && fillIssues[key] == nil {
        let issue = StorageTrendIssue(value: open[key] ?? "")
        alerts.append(Alert(key: storageTrendAlertKey(key), title: storageTrendClearedTitle(issue), text: "", problem: false))
    }
    return StorageTrendOutcome(alerts: alerts, open: next)
}
