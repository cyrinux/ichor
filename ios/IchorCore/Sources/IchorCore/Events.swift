import Foundation

/// One `talosctl events` entry from the Go core (StartEvents).
public struct NodeEvent: Codable, Equatable, Identifiable, Sendable {
    public let node: String
    /// Talos event id (an xid); unique per node.
    public let eventId: String
    /// Unix milliseconds.
    public let at: Int64
    /// service | sequence | phase | task | machine | config | address | restart | other
    public let kind: String
    public let subject: String
    public let action: String
    public let message: String
    /// info | warning | error
    public let severity: String

    public var id: String { "\(node)/\(eventId)" }
    public var eventKind: EventKind { EventKind(rawValue: kind) ?? .other }
    public var eventSeverity: EventSeverity { EventSeverity(rawValue: severity) ?? .info }

    public init(node: String, eventId: String, at: Int64, kind: String, subject: String = "",
                action: String = "", message: String = "", severity: String = "info") {
        self.node = node
        self.eventId = eventId
        self.at = at
        self.kind = kind
        self.subject = subject
        self.action = action
        self.message = message
        self.severity = severity
    }

    private enum CodingKeys: String, CodingKey {
        case node, eventId = "id", at, kind, subject, action, message, severity
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        eventId = try c.field(.eventId, "")
        at = try c.field(.at, 0)
        kind = try c.field(.kind, "other")
        subject = try c.field(.subject, "")
        action = try c.field(.action, "")
        message = try c.field(.message, "")
        severity = try c.field(.severity, "info")
    }

    /// Same node, kind, subject, action and message (time and id aside).
    public func sameContent(as other: NodeEvent) -> Bool {
        node == other.node && kind == other.kind && subject == other.subject
            && action == other.action && message == other.message
    }
}

public enum EventKind: String, CaseIterable, Sendable {
    case service, sequence, phase, task, machine, config, address, restart, other
}

public enum EventSeverity: String, CaseIterable, Sendable {
    case info, warning, error
}

/// Timeline filter: everything, warnings and errors, service events, or the boot sequence.
public enum EventFilter: String, CaseIterable, Sendable {
    case all, problems, services, boot

    public func matches(_ event: NodeEvent) -> Bool {
        switch self {
        case .all: true
        case .problems: event.eventSeverity != .info
        case .services: event.eventKind == .service
        case .boot: [.sequence, .phase, .task].contains(event.eventKind)
        }
    }
}

/// A run of identical consecutive events shown as one row: the newest one, how many, and
/// when the oldest happened.
public struct EventGroup: Equatable, Identifiable, Sendable {
    public let event: NodeEvent
    public let count: Int
    public let firstAt: Int64

    public var id: String { event.id }

    public init(event: NodeEvent, count: Int, firstAt: Int64) {
        self.event = event
        self.count = count
        self.firstAt = firstAt
    }
}

/// Collapses consecutive identical events (newest first in, newest first out). Talos repeats
/// an identical "address" event every 10 minutes, which would otherwise flood the timeline.
public func collapseEvents(_ events: [NodeEvent]) -> [EventGroup] {
    var groups: [EventGroup] = []
    for event in events {
        if let last = groups.last, last.event.sameContent(as: event) {
            groups[groups.count - 1] = EventGroup(event: last.event, count: last.count + 1, firstAt: event.at)
        } else {
            groups.append(EventGroup(event: event, count: 1, firstAt: event.at))
        }
    }
    return groups
}

/// Filters, then collapses, the timeline.
public func eventGroups(_ events: [NodeEvent], filter: EventFilter) -> [EventGroup] {
    collapseEvents(events.filter(filter.matches))
}

/// Returns events with `event` inserted, newest first (by time; a later arrival goes first on
/// a tie), skipping duplicates and keeping at most `cap` (dropping the oldest).
public func insertEvent(_ event: NodeEvent, into events: [NodeEvent], cap: Int = 1000) -> [NodeEvent] {
    if !event.eventId.isEmpty, events.contains(where: { $0.id == event.id }) { return events }
    var low = 0
    var high = events.count
    // First index whose event is not newer than the new one.
    while low < high {
        let mid = (low + high) / 2
        if events[mid].at > event.at { low = mid + 1 } else { high = mid }
    }
    var result = events
    result.insert(event, at: low)
    if result.count > cap { result.removeLast(result.count - cap) }
    return result
}
