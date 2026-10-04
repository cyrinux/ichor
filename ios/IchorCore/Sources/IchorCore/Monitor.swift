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

    public init(context: String, takenAt: Date, nodes: [String: NodeState], etcdAlarms: [String] = [],
                etcdChecked: Bool = false, certNotAfter: Int64 = 0, lastCertWarnDay: Int64 = -1,
                dataWatched: Bool = false, dataChecked: Bool = false, dataIssues: [String: String] = [:], dataPending: [String] = []) {
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
    }

    /// Snapshots saved by older versions lack the data-service fields.
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
public func snapshotOf(_ overview: ClusterOverview, etcd: EtcdOverview?, certNotAfter: Int64, takenAt: Date,
                       dataWatched: Bool = false, dataServices: DataServices? = nil) -> ClusterSnapshot {
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
        dataIssues: dataWatched ? dataServices.map(dataIssuesOf) ?? [:] : [:]
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
/// warning.
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
    default: "CloudNativePG"
    }
}

/// Longhorn, Garage and CloudNativePG issues, diffed like etcd alarms: a critical issue alerts at
/// once, a warning only when seen on two checks in a row; an issue that clears says so once. The
/// first check (or a context switch, or turning watching on) is a silent baseline; a check that
/// could not read them keeps what was known; turning watching off forgets it.
private func evaluateData(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert])
    -> (watched: Bool, checked: Bool, issues: [String: String], pending: [String]) {
    guard current.dataWatched else { return (false, false, [:], []) }
    let known = previous.flatMap { comparable && $0.dataWatched && $0.dataChecked ? $0 : nil }
    guard current.dataChecked else {
        if let known { return (true, true, known.dataIssues, known.dataPending) }
        return (true, false, [:], [])
    }
    guard let known else { return (true, true, current.dataIssues, []) }

    var notified: [String: String] = [:]
    var pending: [String] = []
    for key in current.dataIssues.keys.sorted() {
        let severity = current.dataIssues[key] ?? dataWarning
        let before = known.dataIssues[key]
        if before == severity || (before == dataCritical && severity == dataWarning) {
            // Already notified at this severity or a worse one: quiet.
            notified[key] = severity
        } else if severity == dataCritical || known.dataPending.contains(key) {
            // New or worse critical, or a warning seen for the second time in a row.
            let subject = String(key.split(separator: "|", maxSplits: 1).last ?? Substring(key))
            alerts.append(Alert(key: "data:\(key)", title: "\(subject) needs attention",
                                text: "\(dataSystemTitle(key)) · \(severity)", problem: true))
            notified[key] = severity
        } else {
            pending.append(key)
        }
    }
    for key in known.dataIssues.keys.sorted() where current.dataIssues[key] == nil {
        let subject = String(key.split(separator: "|", maxSplits: 1).last ?? Substring(key))
        alerts.append(Alert(key: "data:\(key)", title: "\(subject) is healthy again", text: dataSystemTitle(key), problem: false))
    }
    return (true, true, notified, pending)
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
        let data = evaluateData(previous: previous, current: current, comparable: comparable, alerts: &alerts)
        next.dataWatched = data.watched
        next.dataChecked = data.checked
        next.dataIssues = data.issues
        next.dataPending = data.pending
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
