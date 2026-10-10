import Foundation

/// What a background alert's notification offers (CYR-36, same table as Android's). Snooze and
/// Wake run in the background and change nothing on the cluster; the others only open the app on
/// the alert's screen, which asks for the usual confirmation before anything is sent.
public enum AlertAction: String, CaseIterable, Hashable, Sendable {
    case wake, reboot, sync, reconcile, silence, snooze

    /// Changes the cluster: never run from the notification, only confirmed in the app.
    public var changesCluster: Bool {
        switch self {
        case .reboot, .sync, .reconcile, .silence: true
        case .wake, .snooze: false
        }
    }
}

/// The actions of an alert's notification: none for a "resolved" or "ready again" one; Wake
/// only for a node with a Wake-on-LAN MAC known (`canWake`). Snooze comes last.
public func alertActions(key: String, problem: Bool, canWake: Bool) -> [AlertAction] {
    guard problem else { return [] }
    let parts = key.split(separator: ":", maxSplits: 1).map(String.init)
    let subject = parts.count > 1 ? parts[1] : ""
    switch parts.first ?? "" {
    case "node": return canWake ? [.wake, .reboot, .snooze] : [.reboot, .snooze]
    case "gitops":
        switch GitOpsSubject(key: subject).tool {
        case "argocd": return [.sync, .snooze]
        case "flux": return [.reconcile, .snooze]
        default: return [.snooze]
        }
    case "am": return [.silence, .snooze]
    case "etcd", "data", "checkup", "storage", "cert", "unreachable": return [.snooze]
    default: return []
    }
}

/// The kind of an alert, which names its notification category: its key's prefix, but
/// "alertmanager" for the Alertmanager's ("am:…") and "cluster" for "unreachable".
public func alertKind(key: String) -> String {
    let prefix = String(key.split(separator: ":", maxSplits: 1).first ?? "")
    switch prefix {
    case "am": return "alertmanager"
    case "unreachable": return "cluster"
    default: return prefix
    }
}

/// The identifier of an alert's notification: its key on `cluster` (its monitor key, see
/// monitorClusterKey), so the same alert on two clusters ("node:10.0.0.2") does not replace the
/// other's. The bare key without a cluster, as older versions posted it.
public func alertNotificationID(cluster: String, alertKey: String) -> String {
    cluster.isEmpty ? alertKey : "\(cluster)|\(alertKey)"
}

/// The notification category of an alert kind ("node", "alertmanager"…) with `actions`:
/// "alert.<kind>" without any (and for the notifications of older versions), else
/// "alert.<kind>.<actions but Snooze, joined by '-'>" or "alert.<kind>.snooze"; ".private" with
/// details hidden.
public func alertCategory(kind: String, actions: [AlertAction], hideDetails: Bool) -> String {
    var id = "alert.\(kind)"
    if !actions.isEmpty {
        let named = actions.filter { $0 != .snooze }.map(\.rawValue)
        id += "." + (named.isEmpty ? AlertAction.snooze.rawValue : named.joined(separator: "-"))
    }
    return hideDetails ? "\(id).private" : id
}

/// Every action set an alert of `kind` can get from `alertActions` (none included), one
/// notification category each.
public func alertActionSets(kind: String) -> [[AlertAction]] {
    switch kind {
    case "node": [[], [.reboot, .snooze], [.wake, .reboot, .snooze]]
    case "gitops": [[], [.sync, .snooze], [.reconcile, .snooze], [.snooze]]
    case "alertmanager": [[], [.silence, .snooze]]
    default: [[], [.snooze]]
    }
}

/// The hours iOS offers to snooze an alert for, one action each.
public let alertSnoozeHours = [1, 8, 24]

/// The action identifier of "Snooze `hours` h".
public func snoozeActionID(hours: Int) -> String { "snooze.\(hours)" }

/// The hours of a Snooze action identifier; nil for another action.
public func snoozeHours(actionID: String) -> Int? {
    guard actionID.hasPrefix("snooze.") else { return nil }
    return Int(actionID.dropFirst("snooze.".count)).flatMap { $0 > 0 ? $0 : nil }
}

/// A notification action that changes the cluster, waiting for its screen: the app opened the
/// alert's link, the screen then shows its own confirmation for `action` on what `alertKey` names.
public struct AlertActionRequest: Equatable, Hashable, Sendable {
    public let action: AlertAction
    public let alertKey: String

    public init(action: AlertAction, alertKey: String) {
        self.action = action
        self.alertKey = alertKey
    }

    private var subject: String {
        let parts = alertKey.split(separator: ":", maxSplits: 1).map(String.init)
        return parts.count > 1 ? parts[1] : ""
    }

    private var prefix: String { String(alertKey.split(separator: ":", maxSplits: 1).first ?? "") }

    /// Reboot of the node at `address` (its talosconfig address).
    public func isReboot(node address: String) -> Bool {
        action == .reboot && prefix == "node" && !address.isEmpty && subject == address
    }

    /// Sync of the Argo CD app `namespace`/`name`.
    public func isSync(namespace: String, name: String) -> Bool {
        let app = GitOpsSubject(key: subject)
        return action == .sync && prefix == "gitops" && app.tool == "argocd" && app.namespace == namespace && app.name == name
    }

    /// Reconcile of the Flux object `kind` `namespace`/`name`.
    public func isReconcile(kind: String, namespace: String, name: String) -> Bool {
        let app = GitOpsSubject(key: subject)
        return action == .reconcile && prefix == "gitops" && app.tool == "flux"
            && app.kind == kind && app.namespace == namespace && app.name == name
    }

    /// The fingerprint of the Alertmanager alert to silence; nil for another request.
    public var silenceFingerprint: String? {
        action == .silence && prefix == "am" && !subject.isEmpty ? subject : nil
    }
}

/// Snoozed alerts, per cluster (its fingerprint) and alert key: until when (unix ms) the monitor
/// posts nothing for that key, neither the problem nor its end. Kept on this device only.
public struct AlertSnoozes: Codable, Equatable, Sendable {
    public private(set) var until: [String: [String: Int64]]

    public init(until: [String: [String: Int64]] = [:]) {
        self.until = until
    }

    /// With `key` of `cluster` snoozed `hours` from `now`.
    public func snoozing(cluster: String, key: String, hours: Int, now: Date) -> AlertSnoozes {
        guard !cluster.isEmpty, !key.isEmpty, hours > 0 else { return self }
        var next = until
        next[cluster, default: [:]][key] = now.addingTimeInterval(TimeInterval(hours) * 3600).epochMillis
        return AlertSnoozes(until: next)
    }

    public func isSnoozed(cluster: String, key: String, now: Date) -> Bool {
        guard let end = until[cluster]?[key] else { return false }
        return end > now.epochMillis
    }

    /// Without the snoozes over at `now`.
    public func pruned(now: Date) -> AlertSnoozes {
        let ms = now.epochMillis
        return AlertSnoozes(until: until.mapValues { $0.filter { $0.value > ms } }.filter { !$0.value.isEmpty })
    }

    /// Only the snoozes of `clusters` (fingerprints): a removed cluster's go with it.
    public func keeping(clusters: [String]) -> AlertSnoozes {
        let kept = Set(clusters)
        return AlertSnoozes(until: until.filter { kept.contains($0.key) })
    }

    /// The `alerts` of `cluster` to post at `now`: the snoozed ones left out.
    public func notSnoozed(_ alerts: [Alert], cluster: String, now: Date) -> [Alert] {
        alerts.filter { !isSnoozed(cluster: cluster, key: $0.key, now: now) }
    }
}
