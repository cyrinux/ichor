import SwiftUI
import IchorCore

/// Live `talosctl events` timeline, newest first, for one node or all of them. Streams only
/// while visible (the task is cancelled when the screen goes away).
struct EventsView: View {
    /// nil: every node of the context.
    let node: String?
    /// Address → hostname, from the overview.
    let hostnames: [String: String]

    static let tail = 50
    static let cap = 1000

    @Environment(AppModel.self) private var model
    @State private var events: [NodeEvent] = []
    @State private var filter = EventFilter.all
    @State private var streaming = false
    @State private var error: String?
    @State private var runID = 0

    var body: some View {
        let groups = eventGroups(events, filter: filter)
        List {
            if let error {
                Section {
                    ErrorOrNoticeText(message: error)
                    Button("Retry") { runID += 1 }
                }
            }
            Section {
                ForEach(groups) { group in
                    EventRow(group: group, hostname: hostname(group.event.node), showNode: node == nil)
                }
            } footer: {
                if !events.isEmpty {
                    Text("Identical consecutive events are shown once with a count.")
                }
            }
        }
        .overlay {
            if groups.isEmpty && error == nil {
                if streaming && events.isEmpty {
                    ProgressView()
                } else {
                    ContentUnavailableView("No events", systemImage: "list.bullet.rectangle",
                                           description: Text("New events appear here as they happen."))
                }
            }
        }
        .themedBackground()
        .navigationTitle(node.map { String(localized: "Events · \(hostname($0))") } ?? String(localized: "Events"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Picker("Show", selection: $filter) {
                        ForEach(EventFilter.allCases, id: \.self) { Text($0.localizedLabel).tag($0) }
                    }
                } label: {
                    Image(systemName: filter == .all
                          ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                    .accessibilityLabel(Text("Filter"))
                    .accessibilityValue(Text(filter.localizedLabel))
                }
            }
        }
        .task(id: runID) { await stream() }
    }

    private func hostname(_ address: String) -> String {
        hostnames[address] ?? address
    }

    private func stream() async {
        guard let client = model.client else { return }
        events = []
        error = nil
        streaming = true
        defer { streaming = false }
        for await item in client.events(node: node, tail: Self.tail) {
            switch item {
            case .event(let event):
                events = insertEvent(event, into: events, cap: Self.cap)
            case .done(let failure):
                if !Task.isCancelled { error = failure }
            }
        }
    }
}

private struct EventRow: View {
    let group: EventGroup
    let hostname: String
    let showNode: Bool

    var body: some View {
        let event = group.event
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: event.eventKind.symbol)
                .foregroundStyle(event.eventSeverity.color)
                .frame(width: 24)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    if let icon = event.eventSeverity.symbol {
                        Image(systemName: icon).foregroundStyle(event.eventSeverity.color).font(.caption)
                    }
                    Text(verbatim: title(event))
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(2)
                    Spacer(minLength: 4)
                    if group.count > 1 {
                        Text(verbatim: "×\(group.count)")
                            .font(.caption.weight(.semibold))
                            .monospacedDigit()
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1)
                            .background(Color.secondary.opacity(0.15), in: Capsule())
                    }
                }
                if !event.message.isEmpty {
                    Text(verbatim: event.message)
                        .font(.caption)
                        .foregroundStyle(event.eventSeverity == .info ? Color.secondary : event.eventSeverity.color)
                        .textSelection(.enabled)
                }
                HStack(spacing: 8) {
                    Text(verbatim: eventTime(event.at))
                    if group.count > 1 {
                        Text("since \(eventTime(group.firstAt))")
                    }
                    if showNode {
                        Text(verbatim: hostname).lineLimit(1)
                    }
                }
                .font(.caption2)
                .foregroundStyle(.secondary)
                .monospacedDigit()
            }
        }
        .padding(.vertical, 2)
    }

    private func title(_ event: NodeEvent) -> String {
        let parts = [event.subject.isEmpty ? event.kind : event.subject, event.action].filter { !$0.isEmpty }
        return parts.joined(separator: " · ")
    }
}

/// Time of day, with the date when it is not today.
private func eventTime(_ millis: Int64) -> String {
    let date = Date(epochMillis: millis)
    if Calendar.current.isDateInToday(date) {
        return date.formatted(date: .omitted, time: .standard)
    }
    return date.formatted(.dateTime.month(.abbreviated).day().hour().minute().second())
}

extension EventKind {
    var symbol: String {
        switch self {
        case .service: "gearshape.2"
        case .sequence: "list.number"
        case .phase: "flag"
        case .task: "checklist"
        case .machine: "server.rack"
        case .config: "doc.badge.gearshape"
        case .address: "network"
        case .restart: "arrow.clockwise"
        case .other: "circle.dashed"
        }
    }
}

extension EventSeverity {
    var color: Color {
        switch self {
        case .info: .accentColor
        case .warning: .orange
        case .error: .red
        }
    }

    var symbol: String? {
        switch self {
        case .info: nil
        case .warning: "exclamationmark.triangle.fill"
        case .error: "xmark.octagon.fill"
        }
    }
}

extension EventFilter {
    var localizedLabel: String {
        switch self {
        case .all: String(localized: "All")
        case .problems: String(localized: "Problems")
        case .services: String(localized: "Services")
        case .boot: String(localized: "Boot")
        }
    }
}
