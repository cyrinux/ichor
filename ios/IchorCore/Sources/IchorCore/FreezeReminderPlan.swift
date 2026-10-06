import Foundation

/// The identifier of a freeze's reminder. One per freeze: two freezes of a project ending
/// together each get their reminder; `prefix` + cluster + "|" starts every one of a cluster's.
public func freezeReminderID(prefix: String, cluster: String, namespace: String, project: String, window: String) -> String {
    "\(prefix)\(cluster)|\(namespace)/\(project)/\(window)"
}

/// Seconds until a freeze's reminder fires, `lead` seconds before it ends (`endsAt`, Unix ms);
/// nil once inside the lead time, when a pending reminder (if any) stays as it is.
public func freezeReminderDelay(endsAt: Int64, lead: TimeInterval, now: Date) -> TimeInterval? {
    let fireIn = TimeInterval(endsAt) / 1000 - lead - now.timeIntervalSince1970
    return fireIn > 1 ? fireIn : nil
}
