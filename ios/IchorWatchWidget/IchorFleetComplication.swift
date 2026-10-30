import IchorCore
import SwiftUI
import WidgetKit

/// The fleet complication: how many clusters need attention, from the tally of the iPhone's
/// last check (WatchPayload.fleet; none from an older iPhone app). Counts only: no cluster name.
struct FleetComplicationEntry: TimelineEntry {
    let date: Date
    let fleet: FleetTally?
}

struct FleetComplicationProvider: TimelineProvider {
    private func current(at date: Date) -> FleetComplicationEntry {
        FleetComplicationEntry(
            date: date, fleet: WatchPayload.load(from: UserDefaults(suiteName: SharedSnapshot.suite))?.fleet)
    }

    func placeholder(in context: Context) -> FleetComplicationEntry {
        FleetComplicationEntry(date: .now, fleet: FleetTally(bad: 1, warn: 1, ok: 4, total: 6, takenAt: .now))
    }

    func getSnapshot(in context: Context, completion: @escaping (FleetComplicationEntry) -> Void) {
        completion(context.isPreview ? placeholder(in: context) : current(at: .now))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<FleetComplicationEntry>) -> Void) {
        let now = Date.now
        let entry = current(at: now)
        var entries = [entry]
        // A second entry turns it stale on time, without waiting for a reload.
        if let takenAt = entry.fleet?.takenAt {
            let staleAt = takenAt.addingTimeInterval(glanceStaleAfter + 1)
            if staleAt > now { entries.append(FleetComplicationEntry(date: staleAt, fleet: entry.fleet)) }
        }
        // The watch app reloads it whenever the phone sends a check; this is a fallback.
        completion(Timeline(entries: entries, policy: .after(now.addingTimeInterval(30 * 60))))
    }
}

struct FleetComplicationView: View {
    @Environment(\.widgetFamily) private var family
    let entry: FleetComplicationEntry

    private var fleet: FleetTally? { entry.fleet.flatMap { $0.total > 0 ? $0 : nil } }
    private var tint: Color { fleet.map { WatchStyle.color($0.tone(at: entry.date)) } ?? WatchStyle.unknown }

    var body: some View {
        content.containerBackground(for: .widget) { Color.clear }
    }

    @ViewBuilder private var content: some View {
        switch family {
        case .accessoryCircular: circular
        case .accessoryInline: inline
        default: rectangular
        }
    }

    private var circular: some View {
        Gauge(value: Double(fleet?.needAttention ?? 0), in: 0...Double(max(fleet?.total ?? 1, 1))) {
            Image(systemName: "square.stack.3d.up")
        } currentValueLabel: {
            if let fleet {
                Text(verbatim: "\(fleet.needAttention)/\(fleet.total)").minimumScaleFactor(0.6)
            } else {
                Text(verbatim: "–")
            }
        }
        .gaugeStyle(.accessoryCircular)
        .tint(tint)
        .accessibilityLabel(fleet.map(WatchStyle.fleet) ?? Text("No check yet"))
    }

    @ViewBuilder private var inline: some View {
        if let fleet {
            WatchStyle.fleet(fleet)
        } else {
            Text("No check yet")
        }
    }

    private var rectangular: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(spacing: 4) {
                Circle().fill(tint).frame(width: 7, height: 7).accessibilityHidden(true)
                Text("All clusters").font(.headline).lineLimit(1)
            }
            if let fleet {
                WatchStyle.fleet(fleet).font(.body).lineLimit(1)
                if let takenAt = fleet.takenAt, fleet.isStale(now: entry.date) {
                    Text("\(Text(takenAt, style: .relative)) ago").font(.caption).lineLimit(1)
                }
            } else {
                Text("No check yet").font(.caption).lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

struct IchorFleetComplication: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "IchorFleetComplication", provider: FleetComplicationProvider()) { entry in
            FleetComplicationView(entry: entry)
        }
        .configurationDisplayName("Fleet health")
        .description("How many of your clusters need attention, from your iPhone's last check.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

/// The cluster complication and the fleet one.
@main
struct IchorWatchWidgets: WidgetBundle {
    var body: some Widget {
        IchorWatchComplication()
        IchorFleetComplication()
    }
}
