import Foundation

// Data services: kube-green sleep schedules (see DataServices.swift).

public struct KubeGreenStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    /// Problems first.
    public let sleepInfos: [KubeGreenSleepInfo]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        sleepInfos = try c.field(.sleepInfos, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, sleepInfos }
}

public struct KubeGreenSleepInfo: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Cron day-of-week, as written ("1-5").
    public let weekdays: String
    /// "HH:mm", each part cron syntax.
    public let sleepAt: String
    /// Empty: it sleeps until changed by hand.
    public let wakeUpAt: String
    /// IANA, empty is UTC.
    public let timeZone: String
    public let suspendDeployments: Bool
    public let suspendStatefulSets: Bool
    public let suspendCronJobs: Bool
    public let excludeCount: Int
    public let includeCount: Int
    public let patchCount: Int
    /// What the controller did last: SLEEP, WAKE_UP or "" before the first run.
    public let operation: String
    /// Unix ms, 0 when none.
    public let lastScheduleAt: Int64
    /// Unix ms, 0 when the schedule does not parse.
    public let nextSleepAt: Int64
    /// Unix ms, 0 when wakeUpAt is empty.
    public let nextWakeUpAt: Int64
    public let asleep: Bool
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [KubeGreenReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        weekdays = try c.field(.weekdays, "")
        sleepAt = try c.field(.sleepAt, "")
        wakeUpAt = try c.field(.wakeUpAt, "")
        timeZone = try c.field(.timeZone, "")
        // Absent means true, as upstream defaults.
        suspendDeployments = try c.field(.suspendDeployments, true)
        suspendStatefulSets = try c.field(.suspendStatefulSets, true)
        suspendCronJobs = try c.field(.suspendCronJobs, false)
        excludeCount = try c.field(.excludeCount, 0)
        includeCount = try c.field(.includeCount, 0)
        patchCount = try c.field(.patchCount, 0)
        operation = try c.field(.operation, "")
        lastScheduleAt = try c.field(.lastScheduleAt, 0)
        nextSleepAt = try c.field(.nextSleepAt, 0)
        nextWakeUpAt = try c.field(.nextWakeUpAt, 0)
        asleep = try c.field(.asleep, false)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, weekdays, sleepAt, wakeUpAt, timeZone, suspendDeployments, suspendStatefulSets, suspendCronJobs
        case excludeCount, includeCount, patchCount, operation, lastScheduleAt, nextSleepAt, nextWakeUpAt, asleep, health, reasons
    }
}

/// Why a SleepInfo is not ok, as the Go core names it.
public enum KubeGreenReason: String, Sendable {
    case noWakeUp, invalidSchedule, overdue, neverRan
}

public extension KubeGreenStatus {
    /// Sleep schedules are the items; one that needs a look is a warning, never critical.
    var summary: ServiceSummary {
        if !error.isEmpty && sleepInfos.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: error) }
        let healths = sleepInfos.map(\.health)
        return ServiceSummary(total: sleepInfos.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: error)
    }
}
