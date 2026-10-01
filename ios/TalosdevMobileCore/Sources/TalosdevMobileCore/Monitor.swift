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

    public init(context: String, takenAt: Date, nodes: [String: NodeState], etcdAlarms: [String] = [],
                etcdChecked: Bool = false, certNotAfter: Int64 = 0, lastCertWarnDay: Int64 = -1) {
        self.context = context
        self.takenAt = takenAt
        self.nodes = nodes
        self.etcdAlarms = etcdAlarms
        self.etcdChecked = etcdChecked
        self.certNotAfter = certNotAfter
        self.lastCertWarnDay = lastCertWarnDay
    }

    public var readyCount: Int { nodes.values.filter { $0.health == .ready }.count }
    public var notReadyCount: Int { nodes.values.filter { $0.health == .notReady }.count }
    public var unreachableCount: Int { nodes.values.filter { $0.health == .unreachable }.count }
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

public func snapshotOf(_ overview: ClusterOverview, etcd: EtcdOverview?, certNotAfter: Int64, takenAt: Date) -> ClusterSnapshot {
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
        etcdChecked: etcd != nil && etcd?.error == nil,
        certNotAfter: certNotAfter
    )
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
/// silent baseline, and the certificate warning fires at most once a day.
public func evaluate(previous: ClusterSnapshot?, current: ClusterSnapshot, now: Date) -> (alerts: [Alert], next: ClusterSnapshot) {
    var alerts: [Alert] = []

    if let previous, previous.context == current.context {
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

    var next = current
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
