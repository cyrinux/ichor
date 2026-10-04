import Foundation

/// One status item on the widget's bottom row: a colored dot and a short text.
public enum GlanceItem: Equatable, Sendable {
    case notReady(Int)
    case unreachable(Int)
    case allReady
    case etcdNoAlarms
    case etcdAlarms(Int)

    public enum Tone: Equatable, Sendable { case ok, warning, problem }

    public var tone: Tone {
        switch self {
        case .notReady: return .warning
        case .unreachable, .etcdAlarms: return .problem
        case .allReady, .etcdNoAlarms: return .ok
        }
    }
}

/// A snapshot older than this is dimmed on the widget and shows its time instead of the row.
public let glanceStaleAfter: TimeInterval = 60 * 60

/// Same rules as the Android widget: node problems first (or "all ready"), then etcd when it
/// was checked, at most two items.
public func glanceItems(_ snapshot: ClusterSnapshot) -> [GlanceItem] {
    var items: [GlanceItem] = []
    if snapshot.notReadyCount > 0 { items.append(.notReady(snapshot.notReadyCount)) }
    if snapshot.unreachableCount > 0 { items.append(.unreachable(snapshot.unreachableCount)) }
    if items.isEmpty { items.append(.allReady) }
    if snapshot.etcdChecked {
        items.append(snapshot.etcdAlarms.isEmpty ? .etcdNoAlarms : .etcdAlarms(snapshot.etcdAlarms.count))
    }
    return Array(items.prefix(2))
}

public func isGlanceStale(_ snapshot: ClusterSnapshot, now: Date) -> Bool {
    now.timeIntervalSince(snapshot.takenAt) > glanceStaleAfter
}

/// The last background check's snapshot, shared by the app and its widget (App Group).
public enum SharedSnapshot {
    public static let suite = "group.name.levis.ichor"
    public static let key = "snapshot"

    /// The snapshot saved in `defaults`, nil when there is none or it does not decode.
    public static func load(from defaults: UserDefaults?) -> ClusterSnapshot? {
        defaults?.data(forKey: key).flatMap { try? JSONDecoder().decode(ClusterSnapshot.self, from: $0) }
    }
}
