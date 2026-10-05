import Foundation

/// What background monitoring last saw; drives alerts (by diffing) and the widget.
public struct ClusterSnapshot: Codable, Equatable, Sendable {
    public var context: String
    public var takenAt: Date
    public var nodes: [String: NodeState]
    public var etcdAlarms: [String]
    public var etcdChecked: Bool
    public var certNotAfter: Int64
    /// Day (unix days) of the last certificate warning, so it fires at most once a day.
    public var lastCertWarnDay: Int64
    /// Watching Longhorn, Garage and CloudNativePG was on for this check (opt-in).
    public var dataWatched: Bool
    /// Their health could be read this time.
    public var dataChecked: Bool
    /// Data-service issues ("system|label" → severity, see dataIssuesOf): observed in a fresh
    /// snapshot; after evaluate, the ones already notified (or present at the baseline).
    public var dataIssues: [String: String]
    /// Warnings seen once and not notified yet: a short rebuild after a reboot should not alert.
    public var dataPending: [String]
    /// Watching Argo CD and Flux apps was on for this check (opt-in), and they could be read.
    public var gitopsWatched: Bool
    public var gitopsChecked: Bool
    /// GitOps app issues ("tool|subject" → "severity:reason", see gitopsIssuesOf), kept like dataIssues.
    public var gitopsIssues: [String: String]
    public var gitopsPending: [String]

    public init(context: String, takenAt: Date, nodes: [String: NodeState], etcdAlarms: [String] = [],
                etcdChecked: Bool = false, certNotAfter: Int64 = 0, lastCertWarnDay: Int64 = -1,
                dataWatched: Bool = false, dataChecked: Bool = false, dataIssues: [String: String] = [:], dataPending: [String] = [],
                gitopsWatched: Bool = false, gitopsChecked: Bool = false, gitopsIssues: [String: String] = [:],
                gitopsPending: [String] = []) {
        self.context = context
        self.takenAt = takenAt
        self.nodes = nodes
        self.etcdAlarms = etcdAlarms
        self.etcdChecked = etcdChecked
        self.certNotAfter = certNotAfter
        self.lastCertWarnDay = lastCertWarnDay
        self.dataWatched = dataWatched
        self.dataChecked = dataChecked
        self.dataIssues = dataIssues
        self.dataPending = dataPending
        self.gitopsWatched = gitopsWatched
        self.gitopsChecked = gitopsChecked
        self.gitopsIssues = gitopsIssues
        self.gitopsPending = gitopsPending
    }

    /// Snapshots saved by older versions lack the data-service and GitOps fields.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.decode(String.self, forKey: .context)
        takenAt = try c.decode(Date.self, forKey: .takenAt)
        nodes = try c.decode([String: NodeState].self, forKey: .nodes)
        etcdAlarms = try c.decodeIfPresent([String].self, forKey: .etcdAlarms) ?? []
        etcdChecked = try c.decodeIfPresent(Bool.self, forKey: .etcdChecked) ?? false
        certNotAfter = try c.decodeIfPresent(Int64.self, forKey: .certNotAfter) ?? 0
        lastCertWarnDay = try c.decodeIfPresent(Int64.self, forKey: .lastCertWarnDay) ?? -1
        dataWatched = try c.decodeIfPresent(Bool.self, forKey: .dataWatched) ?? false
        dataChecked = try c.decodeIfPresent(Bool.self, forKey: .dataChecked) ?? false
        dataIssues = try c.decodeIfPresent([String: String].self, forKey: .dataIssues) ?? [:]
        dataPending = try c.decodeIfPresent([String].self, forKey: .dataPending) ?? []
        gitopsWatched = try c.decodeIfPresent(Bool.self, forKey: .gitopsWatched) ?? false
        gitopsChecked = try c.decodeIfPresent(Bool.self, forKey: .gitopsChecked) ?? false
        gitopsIssues = try c.decodeIfPresent([String: String].self, forKey: .gitopsIssues) ?? [:]
        gitopsPending = try c.decodeIfPresent([String].self, forKey: .gitopsPending) ?? []
    }

    var dataTrack: IssueTrack {
        get { IssueTrack(watched: dataWatched, checked: dataChecked, issues: dataIssues, pending: dataPending) }
        set { (dataWatched, dataChecked, dataIssues, dataPending) = (newValue.watched, newValue.checked, newValue.issues, newValue.pending) }
    }

    var gitopsTrack: IssueTrack {
        get { IssueTrack(watched: gitopsWatched, checked: gitopsChecked, issues: gitopsIssues, pending: gitopsPending) }
        set { (gitopsWatched, gitopsChecked, gitopsIssues, gitopsPending) = (newValue.watched, newValue.checked, newValue.issues, newValue.pending) }
    }

    public var readyCount: Int { nodes.values.filter { $0.health == .ready }.count }
    public var notReadyCount: Int { nodes.values.filter { $0.health == .notReady }.count }
    public var unreachableCount: Int { nodes.values.filter { $0.health == .unreachable }.count }

    /// No node answered: most likely the phone is off the cluster's network (VPN, home LAN).
    public var unreachableAsAWhole: Bool { !nodes.isEmpty && unreachableCount == nodes.count }
}

extension NodeHealth: Codable {}

public struct NodeState: Codable, Equatable, Sendable {
    public var hostname: String
    public var health: NodeHealth
    public var reason: String

    public init(hostname: String, health: NodeHealth, reason: String = "") {
        self.hostname = hostname
        self.health = health
        self.reason = reason
    }
}

/// dataWatched: watching data services was on; dataServices: nil when watched but unreadable.
/// gitopsWatched: watching GitOps apps was on; gitopsIssues (see gitopsIssuesOf): nil when unreadable.
public func snapshotOf(_ overview: ClusterOverview, etcd: EtcdOverview?, certNotAfter: Int64, takenAt: Date,
                       dataWatched: Bool = false, dataServices: DataServices? = nil,
                       gitopsWatched: Bool = false, gitopsIssues: [String: String]? = nil) -> ClusterSnapshot {
    var nodes: [String: NodeState] = [:]
    for n in overview.nodes {
        let reason = n.error ?? n.unmetConditions.map { "\($0.name): \($0.reason)" }.joined(separator: "; ")
        nodes[n.node] = NodeState(hostname: n.hostname, health: n.health, reason: reason)
    }
    return ClusterSnapshot(
        context: overview.context,
        takenAt: takenAt,
        nodes: nodes,
        etcdAlarms: (etcd?.alarms.map { "\($0.memberId):\($0.alarm)" } ?? []).sorted(),
        // A failed alarm list is "not checked", never an all-clear.
        etcdChecked: etcd != nil && etcd?.error == nil && etcd?.alarmsError == nil,
        certNotAfter: certNotAfter,
        dataWatched: dataWatched,
        dataChecked: dataWatched && dataServices != nil,
        dataIssues: dataWatched ? dataServices.map(dataIssuesOf) ?? [:] : [:],
        gitopsWatched: gitopsWatched,
        gitopsChecked: gitopsWatched && gitopsIssues != nil,
        gitopsIssues: gitopsWatched ? gitopsIssues ?? [:] : [:]
    )
}

/// Severities of a data-service issue in a snapshot.
public let dataCritical = "critical"
public let dataWarning = "warning"

/// The problems worth a notification, keyed "system|label" with their severity (same rules as
/// Android): a faulted volume, an unavailable Garage cluster or a Postgres cluster without any
/// instance are critical; a degraded volume, a degraded Garage cluster (or blocks failing to
/// resync) and failing Postgres backups or archiving are warnings. A switchover or a missing
/// replica is usually planned and does not alert. An expired certificate (or one not ready a week
/// before it expires) is critical; one expiring, overdue or not ready, or an issuer not ready, is a
/// warning. A Velero schedule whose last backup failed or whose storage location is unavailable is
/// critical, a stale, partially failed or invalid one a warning. A Ceph cluster alerts when
/// critical, or on HEALTH_WARN, near-full capacity or an OSD down; a pool only when failed.
public func dataIssuesOf(_ services: DataServices) -> [String: String] {
    var out: [String: String] = [:]
    for v in services.longhorn?.volumes ?? [] {
        if v.health == .critical { out["longhorn|\(v.label)"] = dataCritical } else if v.health == .warning { out["longhorn|\(v.label)"] = dataWarning }
    }
    for g in services.garage?.instances ?? [] {
        if g.state == .unavailable {
            out["garage|\(g.label)"] = dataCritical
        } else if g.state == .degraded || g.resyncErrors > 0 {
            out["garage|\(g.label)"] = dataWarning
        }
    }
    let alerting: Set<CnpgReason> = [.archiving, .backupFailed, .backupStale]
    for c in services.cnpg?.clusters ?? [] {
        if c.health == .critical {
            out["cnpg|\(c.label)"] = dataCritical
        } else if c.reasons.contains(where: alerting.contains) {
            out["cnpg|\(c.label)"] = dataWarning
        }
    }
    for d in services.dragonfly?.instances ?? [] {
        if d.health == .critical {
            out["dragonfly|\(d.label)"] = dataCritical
        } else if d.reasons.contains(.pods) || d.reasons.contains(.masters) {
            // A rolling update is planned; a replica down or two masters are not.
            out["dragonfly|\(d.label)"] = dataWarning
        }
    }
    // A replica rolling out or a busy operator is usually planned; a Galera recovery or backups are not.
    let mariadbAlerting: Set<MariaDbReason> = [.galeraRecovery, .backupFailed, .backupStale]
    for m in services.mariadb?.clusters ?? [] {
        if m.health == .critical {
            out["mariadb|\(m.label)"] = dataCritical
        } else if m.reasons.contains(where: mariadbAlerting.contains) {
            out["mariadb|\(m.label)"] = dataWarning
        }
    }
    // The operator still initializing is not worth an alert; a member lost or backups failing are.
    let perconaAlerting: Set<PerconaReason> = [.members, .backupFailed, .backupStale]
    for c in services.percona?.clusters ?? [] {
        if c.health == .critical {
            out["percona|\(c.label)"] = dataCritical
        } else if c.reasons.contains(where: perconaAlerting.contains) {
            out["percona|\(c.label)"] = dataWarning
        }
    }
    // An issuer not ready alerts on its own, not through each of its certificates.
    let certAlerting: Set<CertReason> = [.expiring, .renewalOverdue, .notReady]
    for c in services.certManager?.certificates ?? [] {
        if c.health == .critical {
            out["certmanager|\(c.label)"] = dataCritical
        } else if c.reasons.contains(where: certAlerting.contains) {
            out["certmanager|\(c.label)"] = dataWarning
        }
    }
    for i in services.certManager?.issuers ?? [] where !i.ready {
        out["certmanager|\(i.label)"] = dataWarning
    }
    let veleroAlerting: Set<VeleroReason> = [.stale, .partiallyFailed, .invalid]
    for s in services.velero?.schedules ?? [] {
        if s.health == .critical {
            out["velero|\(s.label)"] = dataCritical
        } else if s.reasons.contains(where: veleroAlerting.contains) {
            out["velero|\(s.label)"] = dataWarning
        }
    }
    // A failed backup taken by hand was seen by whoever took it: shown, not alerted.
    for l in services.velero?.locations ?? [] where l.health == .critical {
        out["velero|BackupStorageLocation/\(l.label)"] = dataCritical
    }
    // A mon down or a reconcile in progress shows in Ceph's health anyway.
    let cephAlerting: Set<CephReason> = [.healthWarn, .nearFull, .osds]
    for c in services.ceph?.clusters ?? [] {
        if c.health == .critical {
            out["ceph|\(c.label)"] = dataCritical
        } else if c.reasons.contains(where: cephAlerting.contains) {
            out["ceph|\(c.label)"] = dataWarning
        }
    }
    // A pool alerts only when Rook reports it failed.
    for p in services.ceph?.pools ?? [] where p.health == .critical { out["ceph|\(p.kind)/\(p.label)"] = dataCritical }
    return out
}

/// Product name of a data-service issue key's system.
private func dataSystemTitle(_ key: String) -> String {
    switch key.split(separator: "|", maxSplits: 1).first.map(String.init) ?? "" {
    case "longhorn": "Longhorn"
    case "garage": "Garage"
    case "dragonfly": "Dragonfly"
    case "mariadb": "MariaDB"
    case "percona": "Percona XtraDB Cluster"
    case "certmanager": "cert-manager"
    case "velero": "Velero"
    case "ceph": "Rook Ceph"
    default: "CloudNativePG"
    }
}

/// One opt-in track of issues as a snapshot carries it (data services, GitOps apps).
struct IssueTrack: Equatable {
    var watched: Bool
    var checked: Bool
    /// Key → value; the value's severity is read with the track's `severity`.
    var issues: [String: String]
    var pending: [String]
}

/// A track's issues, diffed like etcd alarms: a critical issue alerts at once, a warning only when
/// seen on two checks in a row; an issue that clears says so once. The first check (or a context
/// switch, or turning watching on) is a silent baseline; a check that could not read them keeps
/// what was known; turning watching off forgets it.
func evaluateTrack(previous: IssueTrack?, current: IssueTrack, comparable: Bool, severity: (String) -> String,
                   problem: (_ key: String, _ value: String) -> Alert, cleared: (_ key: String) -> Alert,
                   alerts: inout [Alert]) -> IssueTrack {
    guard current.watched else { return IssueTrack(watched: false, checked: false, issues: [:], pending: []) }
    let known = previous.flatMap { comparable && $0.watched && $0.checked ? $0 : nil }
    guard current.checked else {
        if let known { return IssueTrack(watched: true, checked: true, issues: known.issues, pending: known.pending) }
        return IssueTrack(watched: true, checked: false, issues: [:], pending: [])
    }
    guard let known else { return IssueTrack(watched: true, checked: true, issues: current.issues, pending: []) }

    var notified: [String: String] = [:]
    var pending: [String] = []
    for key in current.issues.keys.sorted() {
        let value = current.issues[key] ?? dataWarning
        let level = severity(value)
        let before = known.issues[key].map(severity)
        if before == level || (before == dataCritical && level == dataWarning) {
            // Already notified at this severity or a worse one: quiet.
            notified[key] = value
        } else if level == dataCritical || known.pending.contains(key) {
            // New or worse critical, or a warning seen for the second time in a row.
            alerts.append(problem(key, value))
            notified[key] = value
        } else {
            pending.append(key)
        }
    }
    for key in known.issues.keys.sorted() where current.issues[key] == nil {
        alerts.append(cleared(key))
    }
    return IssueTrack(watched: true, checked: true, issues: notified, pending: pending)
}

/// Longhorn, Garage, CloudNativePG… issues (see evaluateTrack), keyed "data:system|label".
private func evaluateData(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert]) -> IssueTrack {
    func subject(_ key: String) -> String { String(key.split(separator: "|", maxSplits: 1).last ?? Substring(key)) }
    return evaluateTrack(
        previous: previous?.dataTrack, current: current.dataTrack, comparable: comparable, severity: { $0 },
        problem: { key, severity in
            Alert(key: "data:\(key)", title: "\(subject(key)) needs attention", text: "\(dataSystemTitle(key)) · \(severity)", problem: true)
        },
        cleared: { key in Alert(key: "data:\(key)", title: "\(subject(key)) is healthy again", text: dataSystemTitle(key), problem: false) },
        alerts: &alerts)
}

/// `key` identifies the subject, so a newer alert replaces the older notification.
public struct Alert: Equatable, Sendable {
    public let key: String
    public let title: String
    public let text: String
    public let problem: Bool
}

/// Days before expiry when the daily "renew your talosconfig" alert starts.
public let certWarnDays = 7

/// Same rules as Android: only changes alert, a first snapshot (or a context switch) is a
/// silent baseline, and the certificate warning fires at most once a day. A check where no
/// node answered says nothing about the cluster (the phone is off its network): it alerts
/// nothing and keeps the previous snapshot.
public func evaluate(previous: ClusterSnapshot?, current: ClusterSnapshot, now: Date) -> (alerts: [Alert], next: ClusterSnapshot) {
    var alerts: [Alert] = []
    var next = current
    let comparable = previous.map { $0.context == current.context && !$0.unreachableAsAWhole } ?? false

    if let previous, comparable, current.unreachableAsAWhole {
        next = previous
        next.certNotAfter = current.certNotAfter
    } else {
        next.dataTrack = evaluateData(previous: previous, current: current, comparable: comparable, alerts: &alerts)
        next.gitopsTrack = evaluateGitOps(previous: previous, current: current, comparable: comparable, alerts: &alerts)
    }
    if let previous, comparable, !current.unreachableAsAWhole {
        for addr in current.nodes.keys.sorted() {
            guard let state = current.nodes[addr], let before = previous.nodes[addr], before.health != state.health else { continue }
            let reason = state.reason.isEmpty ? addr : state.reason
            switch state.health {
            case .ready: alerts.append(Alert(key: "node:\(addr)", title: "\(state.hostname) is ready again", text: addr, problem: false))
            case .notReady: alerts.append(Alert(key: "node:\(addr)", title: "\(state.hostname) is not ready", text: reason, problem: true))
            case .unreachable: alerts.append(Alert(key: "node:\(addr)", title: "\(state.hostname) is unreachable", text: reason, problem: true))
            }
        }
        if current.etcdChecked && previous.etcdChecked {
            for alarm in current.etcdAlarms where !previous.etcdAlarms.contains(alarm) {
                let parts = alarm.split(separator: ":", maxSplits: 1).map(String.init)
                alerts.append(Alert(key: "etcd:\(alarm)", title: "etcd alarm raised",
                                    text: "\(parts.last ?? alarm) on member \(parts.first ?? "")", problem: true))
            }
        }
    }

    let today = Int64((now.timeIntervalSince1970 / 86_400).rounded(.down))
    let lastWarn = previous?.lastCertWarnDay ?? -1
    next.lastCertWarnDay = lastWarn
    if current.certNotAfter > 0 && lastWarn != today {
        let days = daysUntil(current.certNotAfter, now: now)
        if days <= certWarnDays {
            let text = days < 0 ? "The client certificate expired \(-days) days ago."
                : "The client certificate expires in \(days) days. Generate a new talosconfig."
            alerts.append(Alert(key: "cert", title: "talosconfig certificate", text: text, problem: true))
            next.lastCertWarnDay = today
        }
    }
    return (alerts, next)
}
