import IchorCore
import SwiftUI
import WidgetKit

/// The watch face complication (CYR-339): the cluster open on the iPhone, from the last payload
/// the watch app kept (WatchModel). Same rules as the iOS widget (glanceItems, glanceStaleAfter);
/// a stale check shows its age instead.
struct WatchEntry: TimelineEntry {
    let date: Date
    let cluster: WatchCluster?
}

struct WatchProvider: TimelineProvider {
    private func current(at date: Date) -> WatchEntry {
        WatchEntry(
            date: date, cluster: WatchPayload.load(from: UserDefaults(suiteName: SharedSnapshot.suite))?.activeCluster)
    }

    func placeholder(in context: Context) -> WatchEntry {
        WatchEntry(
            date: .now,
            cluster: WatchCluster(
                id: "0", name: "Lab", active: true, takenAt: .now,
                nodes: [WatchNode(name: "node", health: .ready)], etcdChecked: true))
    }

    func getSnapshot(in context: Context, completion: @escaping (WatchEntry) -> Void) {
        completion(context.isPreview ? placeholder(in: context) : current(at: .now))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<WatchEntry>) -> Void) {
        let now = Date.now
        let entry = current(at: now)
        var entries = [entry]
        // A second entry turns it stale on time, without waiting for a reload.
        if let takenAt = entry.cluster?.takenAt {
            let staleAt = takenAt.addingTimeInterval(glanceStaleAfter + 1)
            if staleAt > now { entries.append(WatchEntry(date: staleAt, cluster: entry.cluster)) }
        }
        // The watch app reloads it whenever the phone sends a check; this is a fallback.
        completion(Timeline(entries: entries, policy: .after(now.addingTimeInterval(30 * 60))))
    }
}

struct WatchComplicationView: View {
    @Environment(\.widgetFamily) private var family
    let entry: WatchEntry

    private var stale: Bool { entry.cluster?.isStale(now: entry.date) ?? true }

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
        ZStack {
            AccessoryWidgetBackground()
            VStack(spacing: 1) {
                Image(systemName: "server.rack")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(stale ? WatchStyle.unknown : WatchStyle.color(entry.cluster?.tone))
                if let cluster = entry.cluster, cluster.takenAt != nil {
                    Text(verbatim: "\(cluster.readyCount)/\(cluster.nodes.count)")
                        .font(.system(size: 13, weight: .semibold))
                        .minimumScaleFactor(0.6)
                } else {
                    Text(verbatim: "–")
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var inline: some View {
        if let cluster = entry.cluster, let takenAt = cluster.takenAt {
            if stale {
                Text(verbatim: "\(cluster.name) · ") + Text("\(Text(takenAt, style: .relative)) ago")
            } else if let first = cluster.glance.first {
                Text(verbatim: "\(cluster.name) · ") + WatchStyle.text(first)
            }
        } else {
            Text(verbatim: entry.cluster?.name ?? "Ichor")
        }
    }

    private var rectangular: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(spacing: 4) {
                Circle()
                    .fill(stale ? WatchStyle.unknown : WatchStyle.color(entry.cluster?.tone))
                    .frame(width: 7, height: 7)
                    .accessibilityHidden(true)
                Text(verbatim: entry.cluster?.name ?? "Ichor").font(.headline).lineLimit(1)
            }
            if let cluster = entry.cluster, let takenAt = cluster.takenAt {
                WatchStyle.ready(cluster).font(.body).lineLimit(1)
                if stale {
                    Text("\(Text(takenAt, style: .relative)) ago").font(.caption).lineLimit(1)
                } else if let first = cluster.glance.first {
                    WatchStyle.text(first).font(.caption).lineLimit(1)
                }
            } else {
                Text("No check yet").font(.caption).lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

@main
struct IchorWatchComplication: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "IchorWatchComplication", provider: WatchProvider()) { entry in
            WatchComplicationView(entry: entry)
        }
        .configurationDisplayName("Cluster health")
        .description("The open cluster's nodes and etcd alarms from your iPhone's last check.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}
