import Foundation

// Mirrors go/ichorgo/kube_cronjobs.go.

public struct KubeCronJobList: Decodable, Equatable, Sendable {
    public let cronJobs: [KubeCronJob]

    public init(cronJobs: [KubeCronJob] = []) { self.cronJobs = cronJobs }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cronJobs = try c.field(.cronJobs, [])
    }

    private enum CodingKeys: String, CodingKey { case cronJobs }
}

/// A CronJob with its schedule, recent runs and the ichor.levis.name/* settings (KubeCronJobs).
public struct KubeCronJob: Codable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// ichor.levis.name/title; "" when not set.
    public let title: String
    /// ichor.levis.name/description; "" when not set.
    public let description: String
    /// Bundled icon name, nil when none.
    public let icon: String?
    /// Dashboard Icons slug, downloaded only when the user allowed it; nil when none.
    public let remoteIcon: String?
    public let schedule: String
    public let timeZone: String
    public let suspended: Bool
    /// False when ichor.levis.name/trigger=false: runs on schedule only.
    public let triggerable: Bool
    public let active: Int
    /// The latest run's: running, succeeded, failed or never.
    public let state: String
    /// Unix ms, 0 when never.
    public let lastSchedule: Int64
    public let lastSuccess: Int64
    /// Unix ms, 0 when suspended or unknown.
    public let nextRun: Int64
    public let images: [String]
    /// Newest first.
    public let runs: [KubeJobRun]

    public var id: String { "\(namespace)/\(name)" }

    /// The title when set, else the CronJob's name.
    public var displayName: String { title.isEmpty ? name : title }

    public var runState: JobRunState { JobRunState(rawValue: state) ?? .never }

    /// For the icon tile: the resolved icon; the view draws a clock when there is none.
    public var iconApp: InventoryApp { InventoryApp(id: "cronjob:\(id)", name: displayName, icon: icon, remoteIcon: remoteIcon) }

    public init(namespace: String, name: String, title: String = "", description: String = "", icon: String? = nil,
                remoteIcon: String? = nil, schedule: String = "", timeZone: String = "", suspended: Bool = false,
                triggerable: Bool = true, active: Int = 0, state: String = "", lastSchedule: Int64 = 0,
                lastSuccess: Int64 = 0, nextRun: Int64 = 0, images: [String] = [], runs: [KubeJobRun] = []) {
        self.namespace = namespace
        self.name = name
        self.title = title
        self.description = description
        self.icon = icon
        self.remoteIcon = remoteIcon
        self.schedule = schedule
        self.timeZone = timeZone
        self.suspended = suspended
        self.triggerable = triggerable
        self.active = active
        self.state = state
        self.lastSchedule = lastSchedule
        self.lastSuccess = lastSuccess
        self.nextRun = nextRun
        self.images = images
        self.runs = runs
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.decode(String.self, forKey: .namespace)
        name = try c.decode(String.self, forKey: .name)
        title = try c.field(.title, "")
        description = try c.field(.description, "")
        icon = try c.decodeIfPresent(String.self, forKey: .icon).nonEmpty
        remoteIcon = try c.decodeIfPresent(String.self, forKey: .remoteIcon).nonEmpty
        schedule = try c.field(.schedule, "")
        timeZone = try c.field(.timeZone, "")
        suspended = try c.field(.suspended, false)
        triggerable = try c.field(.triggerable, true)
        active = try c.field(.active, 0)
        state = try c.field(.state, "")
        lastSchedule = try c.field(.lastSchedule, 0)
        lastSuccess = try c.field(.lastSuccess, 0)
        nextRun = try c.field(.nextRun, 0)
        images = try c.field(.images, [])
        runs = try c.field(.runs, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, title, description, icon, remoteIcon, schedule, timeZone, suspended, triggerable
        case active, state, lastSchedule, lastSuccess, nextRun, images, runs
    }
}

/// One Job a CronJob started.
public struct KubeJobRun: Codable, Equatable, Identifiable, Sendable {
    public let name: String
    public let state: String
    /// Started by hand (Ichor, kubectl create job --from).
    public let manual: Bool
    /// Unix ms.
    public let started: Int64
    /// Unix ms, 0 while running.
    public let finished: Int64

    public var id: String { name }

    public var runState: JobRunState { JobRunState(rawValue: state) ?? .never }

    /// How long it ran in seconds, nil while it runs or when unknown.
    public var duration: TimeInterval? {
        guard finished > 0, started > 0, finished >= started else { return nil }
        return TimeInterval(finished - started) / 1000
    }

    public init(name: String, state: String = "", manual: Bool = false, started: Int64 = 0, finished: Int64 = 0) {
        self.name = name
        self.state = state
        self.manual = manual
        self.started = started
        self.finished = finished
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        state = try c.field(.state, "")
        manual = try c.field(.manual, false)
        started = try c.field(.started, 0)
        finished = try c.field(.finished, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, state, manual, started, finished }
}

public enum JobRunState: String, Sendable {
    case running, succeeded, failed, never

    /// Running first, then failed, then the rest.
    var attentionRank: Int {
        switch self {
        case .running: 0
        case .failed: 1
        default: 2
        }
    }
}

/// Namespaces that have CronJobs, sorted.
public func cronJobNamespaces(_ cronJobs: [KubeCronJob]) -> [String] {
    Array(Set(cronJobs.map(\.namespace))).sorted()
}

/// CronJobs of namespace (all when nil) whose name, title, description, schedule or image
/// contains query (case-insensitive). sorted: running ones first, then failed, then by
/// namespace and name; else in the order loaded (a list still incomplete).
public func filterCronJobs(_ cronJobs: [KubeCronJob], namespace: String?, query: String, sorted: Bool = true) -> [KubeCronJob] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    let contains = { (text: String) in text.range(of: needle, options: .caseInsensitive) != nil }
    let matching = cronJobs.filter { c in
        (namespace == nil || c.namespace == namespace) &&
            (needle.isEmpty || [c.name, c.title, c.description, c.schedule].contains(where: contains) ||
                c.images.contains(where: contains))
    }
    guard sorted else { return matching }
    return matching
        .sorted { a, b in
            let ra = a.runState.attentionRank, rb = b.runState.attentionRank
            if ra != rb { return ra < rb }
            if a.namespace != b.namespace { return a.namespace < b.namespace }
            return a.name < b.name
        }
}
