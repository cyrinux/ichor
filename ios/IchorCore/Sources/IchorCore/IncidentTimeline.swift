import Foundation

// The recorder's entries as a timeline of one-line rows: a health per entry, runs of healthy
// service rows folded into one, and each time as an offset from the start of the recording.
// Only the rendering: the stored entries are unchanged. Same rules as Android's
// ui/insights/IncidentTimeline.kt.

public extension IncidentEntry {
    /// The row's dot: critical when the node is unreachable or the evidence failed, warning
    /// when a condition changed for the worse (not ready, unhealthy, a link change, a warning,
    /// metric errors or drops), ok otherwise.
    var timelineHealth: ServiceHealth {
        let detail = evidenceDetail ?? [:]
        func text(_ key: String) -> String { detail[key] as? String ?? "" }
        let reachable = detail["reachable"] as? Bool
        let ready = detail["ready"] as? Bool
        if severity == "error" || kind == "error" || (kind == "status" && reachable == false) {
            return .critical
        }
        let conditions = (detail["conditions"] as? [Any])?.isEmpty == false
        let metrics = kind == "metrics" ? evidenceMetrics : nil
        let metricProblem = metrics.map { m in
            !m.errors.isEmpty || m.network.contains { $0.errors > 0 || $0.drops > 0 }
        } ?? false
        let warning = severity == "warning" || kind == "link" ||
            (kind == "status" && (ready == false || conditions)) ||
            (kind == "service" && (text("health") == "unhealthy" || text("state") == "absent")) ||
            metricProblem
        return warning ? .warning : .ok
    }
}

/// One timeline row: an entry, or a run of consecutive healthy service entries of one node.
public enum IncidentTimelineItem: Identifiable, Equatable, Sendable {
    case entry(IncidentEntry)
    case healthyRun([IncidentEntry])

    public var id: String {
        switch self {
        case .entry(let entry): return entry.id
        case .healthyRun(let entries): return "run/" + (entries.first?.id ?? "")
        }
    }

    /// The entries in the row, in timeline order.
    public var entries: [IncidentEntry] {
        switch self {
        case .entry(let entry): return [entry]
        case .healthyRun(let entries): return entries
        }
    }
}

/// The kind whose consecutive healthy entries fold into one "N services healthy" row.
private let collapsibleIncidentKind = "service"

/// Groups `entries` (already in display order) into rows: two or more consecutive ok service
/// entries of the same node become one healthy run; every other entry keeps its own row.
public func incidentTimeline(_ entries: [IncidentEntry]) -> [IncidentTimelineItem] {
    var items: [IncidentTimelineItem] = []
    var run: [IncidentEntry] = []
    func flush() {
        if run.count > 1 {
            items.append(.healthyRun(run))
        } else if let only = run.first {
            items.append(.entry(only))
        }
        run = []
    }
    for entry in entries {
        guard entry.kind == collapsibleIncidentKind, entry.timelineHealth == .ok else {
            flush()
            items.append(.entry(entry))
            continue
        }
        if let first = run.first, first.node != entry.node { flush() }
        run.append(entry)
    }
    flush()
    return items
}

/// The time of an entry from the start of the recording: "+00:07", "+12:30", "+1:02:03"
/// past an hour, "-00:02" for a node clock behind the start.
public func incidentOffset(at: Int64, startedAt: Int64) -> String {
    let millis = at - startedAt
    let seconds = abs(millis) / 1000
    let hours = seconds / 3600
    let minutes = (seconds % 3600) / 60
    let rest = seconds % 60
    let sign = millis < 0 ? "-" : "+"
    func two(_ value: Int64) -> String { value < 10 ? "0\(value)" : "\(value)" }
    return hours > 0 ? "\(sign)\(hours):\(two(minutes)):\(two(rest))" : "\(sign)\(two(minutes)):\(two(rest))"
}
