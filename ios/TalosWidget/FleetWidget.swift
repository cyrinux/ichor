import IchorCore
import SwiftUI
import WidgetKit

/// Every cluster at once: "2 / 6 need attention" in the worst tone's colour, then the first of
/// those clusters with their top finding; a tap opens the fleet screen. Drawn from the glance
/// the app keeps after each background check (SharedFleet): the extension never calls the Go
/// core or a cluster. Same content as Android's FleetWidget.
struct FleetWidgetEntry: TimelineEntry {
    let date: Date
    let glance: FleetGlance?
}

struct FleetProvider: TimelineProvider {
    private func current(at date: Date) -> FleetWidgetEntry {
        FleetWidgetEntry(date: date, glance: SharedFleet.load(from: UserDefaults(suiteName: SharedSnapshot.suite)))
    }

    /// The demo fleet's names, for the gallery: no real cluster name.
    func placeholder(in context: Context) -> FleetWidgetEntry {
        let now = Int64(Date.now.timeIntervalSince1970 * 1000)
        return FleetWidgetEntry(
            date: .now,
            glance: FleetGlance(rows: [
                FleetGlanceRow(key: "1", name: "prod-eu", tone: .bad, checkedAt: now, finding: "Nodes"),
                FleetGlanceRow(key: "2", name: "staging", tone: .warn, checkedAt: now, finding: "Argo CD"),
                FleetGlanceRow(key: "3", name: "lab", tone: .ok, checkedAt: now),
                FleetGlanceRow(key: "4", name: "edge", tone: .ok, checkedAt: now),
            ]))
    }

    func getSnapshot(in context: Context, completion: @escaping (FleetWidgetEntry) -> Void) {
        completion(context.isPreview ? placeholder(in: context) : current(at: .now))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<FleetWidgetEntry>) -> Void) {
        let now = Date.now
        let entry = current(at: now)
        var entries = [entry]
        // One entry per check turning stale, so the count and the colour follow without a reload.
        let staleAts = (entry.glance?.rows ?? []).compactMap(\.checkedDate)
            .map { $0.addingTimeInterval(glanceStaleAfter + 1) }
            .filter { $0 > now }
        for date in Set(staleAts).sorted().prefix(6) {
            entries.append(FleetWidgetEntry(date: date, glance: entry.glance))
        }
        // The app reloads the timelines after each check; this is a fallback.
        completion(Timeline(entries: entries, policy: .after(now.addingTimeInterval(30 * 60))))
    }
}

struct FleetWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: FleetWidgetEntry

    private var view: FleetGlanceView? {
        entry.glance?.view(at: entry.date, maxRows: family == .systemSmall ? 2 : fleetWidgetMaxRows)
    }

    var body: some View {
        Group {
            switch family {
            case .accessoryCircular: circular
            case .accessoryRectangular: lockScreen
            default: card
            }
        }
        .containerBackground(for: .widget) { lockScreenFamily ? Color.clear : Glance.background }
        .widgetURL(fleetWidgetURL)
    }

    private var lockScreenFamily: Bool { family == .accessoryCircular || family == .accessoryRectangular }

    private var card: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("All clusters").font(.system(size: 12)).foregroundStyle(Glance.secondary).lineLimit(1)
            count
                .font(.system(size: 24, weight: .semibold))
                .foregroundStyle(countColor)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            detail
                .font(.system(size: 12))
                .foregroundStyle(Glance.secondary)
                .padding(.top, 2)
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityElement(children: .combine)
    }

    private var lockScreen: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text("All clusters").font(.caption).lineLimit(1)
            count.font(.headline).lineLimit(1)
            if let view, !view.allStale, let first = view.rows.first {
                Text(verbatim: rowText(first)).font(.caption).lineLimit(1)
            } else {
                summary.font(.caption).lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private var circular: some View {
        Gauge(value: Double(view?.attention ?? 0), in: 0...Double(max(view?.total ?? 1, 1))) {
            Image(systemName: "square.stack.3d.up")
        } currentValueLabel: {
            if let view {
                Text(verbatim: "\(view.attention)/\(view.total)").minimumScaleFactor(0.6)
            } else {
                Text(verbatim: "–")
            }
        }
        .gaugeStyle(.accessoryCircular)
        .accessibilityLabel(accessibilitySummary)
    }

    /// "2 / 6", then "need attention" under it.
    private var count: Text {
        guard let view else { return Text(verbatim: "–") }
        return Text(verbatim: "\(view.attention) / \(view.total)")
    }

    private var countColor: Color {
        guard let view else { return Glance.primary }
        if view.allStale { return Glance.primary.opacity(0.5) }
        return color(view.tone)
    }

    @ViewBuilder private var detail: some View {
        if let view, !view.allStale, !view.rows.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text("need attention")
                ForEach(view.rows, id: \.key) { row in
                    HStack(spacing: 5) {
                        Circle().fill(color(row.tone)).frame(width: 7, height: 7).accessibilityHidden(true)
                        Text(verbatim: rowText(row)).lineLimit(1)
                    }
                }
            }
        } else {
            summary.lineLimit(2)
        }
    }

    /// Without rows to list: all fine, how many were not checked lately, or the oldest check's age.
    private var summary: Text {
        guard let view else { return Text("Open the app to set up") }
        if view.allStale {
            guard let oldest = view.oldestCheck else { return Text("Never checked") }
            return Text("Oldest check \(Text(oldest, style: .relative)) ago")
        }
        if view.attention > 0 { return Text("need attention") }
        if view.unchecked > 0 { return Text("Not checked: \(view.unchecked)") }
        return Text("All \(view.total) fine")
    }

    private var accessibilitySummary: Text {
        guard let view else { return Text("Open the app to set up") }
        return Text("\(view.attention) of \(view.total) need attention")
    }

    /// "prod-eu · Nodes: node-b unreachable", or the name alone without a finding.
    private func rowText(_ row: FleetGlanceRow) -> String {
        row.finding.isEmpty ? row.name : "\(row.name) · \(row.finding)"
    }

    private func color(_ tone: FleetTone) -> Color {
        switch tone {
        case .bad: Glance.problem
        case .warn: Glance.warning
        case .unknown: Glance.secondary
        case .ok: Glance.ok
        }
    }
}

/// The Home Screen and Lock Screen fleet widget; IchorWidgets bundles it next to TalosWidget.
struct FleetWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "IchorFleetWidget", provider: FleetProvider()) { entry in
            FleetWidgetView(entry: entry)
        }
        .configurationDisplayName("Fleet status")
        .description("How many of your clusters need attention, from the last background check.")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryRectangular, .accessoryCircular])
    }
}
