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

/// What kube-green has done to one namespace, read when a workload or CronJob row is opened so
/// it can say the namespace is asleep before a scale, a restart or a resume.
/// (go/ichorgo/kube_kubegreen_namespace.go)
public struct KubeGreenNamespace: Decodable, Equatable, Sendable {
    /// False when kube-green is not installed, or the list was refused: nothing to say.
    public let installed: Bool
    public let asleep: Bool
    public let sleepInfos: [KubeGreenSleepInfo]
    public let sleepsDeployments: Bool
    public let sleepsStatefulSets: Bool
    public let sleepsCronJobs: Bool
    /// Unix ms, 0 when none.
    public let nextWakeUpAt: Int64
    /// Unix ms, 0 when none.
    public let nextSleepAt: Int64
    /// A SleepInfo has excludeRef or includeRef: which objects it touches cannot be told.
    public let filtered: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        asleep = try c.field(.asleep, false)
        sleepInfos = try c.field(.sleepInfos, [])
        sleepsDeployments = try c.field(.sleepsDeployments, false)
        sleepsStatefulSets = try c.field(.sleepsStatefulSets, false)
        sleepsCronJobs = try c.field(.sleepsCronJobs, false)
        nextWakeUpAt = try c.field(.nextWakeUpAt, 0)
        nextSleepAt = try c.field(.nextSleepAt, 0)
        filtered = try c.field(.filtered, false)
    }

    private enum CodingKeys: String, CodingKey {
        case installed, asleep, sleepInfos, sleepsDeployments, sleepsStatefulSets, sleepsCronJobs, nextWakeUpAt, nextSleepAt, filtered
    }
}

/// Why kube-green would undo a change made by hand: the namespace is asleep, or sleeps soon.
public enum KubeGreenWarn: Equatable, Sendable { case asleep, sleepsSoon }

public extension KubeGreenNamespace {
    /// The SleepInfo that has the namespace asleep now; nil when none does.
    var asleepInfo: KubeGreenSleepInfo? { sleepInfos.first { $0.asleep } }

    /// Whether kube-green sleeps the kind this row is ("Deployment", "StatefulSet", "CronJob").
    func sleeps(_ kind: String) -> Bool {
        switch kind {
        case "Deployment": sleepsDeployments
        case "StatefulSet": sleepsStatefulSets
        case "CronJob": sleepsCronJobs
        default: false
        }
    }

    /// Whether a change made now is undone by kube-green: `.asleep` while it sleeps the
    /// namespace, `.sleepsSoon` when it sleeps it within a day, nil otherwise. The pieces the
    /// row's wording is built from (see `KubeGreenAsleepLabel`).
    func warnBeforeChange(now: Date = Date()) -> KubeGreenWarn? {
        if asleep { return .asleep }
        let soon = now.epochMillis + 24 * 60 * 60 * 1000
        return nextSleepAt > 0 && nextSleepAt <= soon ? .sleepsSoon : nil
    }

    /// The instant a change made now is undone: the next wake-up while asleep, else the next sleep.
    var changeUndoneAt: Int64 { asleep ? nextWakeUpAt : nextSleepAt }
}

public extension KubeGreenStatus {
    /// Sleep schedules are the items; one that needs a look is a warning, never critical.
    var summary: ServiceSummary {
        if !error.isEmpty && sleepInfos.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: error) }
        let healths = sleepInfos.map(\.health)
        return ServiceSummary(total: sleepInfos.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: error)
    }
}

// The schedule editor's values, checked the way the SleepInfo CRD and the Go core check them,
// so a bad one is refused in the sheet before any request.
public enum KubeGreenSchedule {
    /// The days a cron day-of-week field picks (0 Sunday … 6 Saturday; 7 is Sunday too); nil when
    /// it uses steps or anything else day toggles cannot show, which is then edited as text.
    public static func days(_ weekdays: String) -> Set<Int>? {
        let w = weekdays.trimmingCharacters(in: .whitespaces)
        if w.isEmpty || w == "*" { return Set(0...6) }
        var days = Set<Int>()
        for part in w.split(separator: ",", omittingEmptySubsequences: false) {
            var bounds: [Int] = []
            for b in part.split(separator: "-", omittingEmptySubsequences: false) {
                guard let d = Int(b.trimmingCharacters(in: .whitespaces)), (0...7).contains(d) else { return nil }
                bounds.append(d)
            }
            switch bounds.count {
            case 1: days.insert(bounds[0] % 7)
            case 2:
                guard bounds[0] <= bounds[1] else { return nil }
                for d in bounds[0]...bounds[1] { days.insert(d % 7) }
            default: return nil
            }
        }
        return days
    }

    /// The cron day-of-week field for `days`: "*" for every day, else runs of days ("1-5", "0,6").
    public static func weekdays(_ days: Set<Int>) -> String {
        if Set(0...6).isSubset(of: days) { return "*" }
        var runs: [ClosedRange<Int>] = []
        for d in days.filter({ (0...6).contains($0) }).sorted() {
            if let last = runs.last, last.upperBound + 1 == d {
                runs[runs.count - 1] = last.lowerBound...d
            } else {
                runs.append(d...d)
            }
        }
        return runs.map { $0.lowerBound == $0.upperBound ? "\($0.lowerBound)" : "\($0.lowerBound)-\($0.upperBound)" }.joined(separator: ",")
    }

    /// A sleepAt or wakeUpAt kube-green takes: "HH:mm", each part cron syntax; plain numbers make a real time.
    public static func timeValid(_ at: String) -> Bool {
        let t = at.trimmingCharacters(in: .whitespaces)
        guard t.range(of: #"^[0-9*,/-]+:[0-9*,/-]+$"#, options: .regularExpression) != nil else { return false }
        let parts = t.split(separator: ":")
        if let hour = Int(parts[0]), hour > 23 { return false }
        if let minute = Int(parts[1]), minute > 59 { return false }
        return true
    }

    public static func weekdaysValid(_ weekdays: String) -> Bool {
        weekdays.trimmingCharacters(in: .whitespaces).range(of: #"^[0-6*,/-]+$"#, options: .regularExpression) != nil
    }

    /// An IANA zone the phone knows ("" is UTC).
    public static func zoneValid(_ zone: String) -> Bool {
        let z = zone.trimmingCharacters(in: .whitespaces)
        return z.isEmpty || TimeZone(identifier: z) != nil
    }
}
