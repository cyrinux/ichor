import SwiftUI
import TalosViewerCore
import WidgetKit

/// Home-screen summary of the last background check. Counts only, no hostnames: it stays
/// visible when the app lock is on.
struct Entry: TimelineEntry {
    let date: Date
    let snapshot: ClusterSnapshot?
}

struct Provider: TimelineProvider {
    private var snapshot: ClusterSnapshot? {
        UserDefaults(suiteName: "group.name.levis.talosmobile")?.data(forKey: "snapshot")
            .flatMap { try? JSONDecoder().decode(ClusterSnapshot.self, from: $0) }
    }

    func placeholder(in context: Context) -> Entry { Entry(date: .now, snapshot: nil) }
    func getSnapshot(in context: Context, completion: @escaping (Entry) -> Void) { completion(Entry(date: .now, snapshot: snapshot)) }
    func getTimeline(in context: Context, completion: @escaping (Timeline<Entry>) -> Void) {
        // The app reloads timelines after each check; this is just a fallback refresh.
        completion(Timeline(entries: [Entry(date: .now, snapshot: snapshot)], policy: .after(.now.addingTimeInterval(30 * 60))))
    }
}

struct ClusterWidgetView: View {
    let entry: Entry

    var body: some View {
        if let s = entry.snapshot {
            VStack(alignment: .leading, spacing: 2) {
                Text(s.context).font(.caption).foregroundStyle(.secondary)
                Text("\(s.readyCount)/\(s.nodes.count) ready")
                    .font(.title2.bold())
                    .foregroundStyle(s.readyCount == s.nodes.count ? .green : .orange)
                Text("\(s.notReadyCount) not ready · \(s.unreachableCount) down").font(.caption)
                Text(!s.etcdChecked ? "etcd: unknown" : s.etcdAlarms.isEmpty ? "etcd: no alarms" : "etcd: \(s.etcdAlarms.count) alarm(s)")
                    .font(.caption)
                Spacer(minLength: 0)
                Text("updated \(s.takenAt, style: .time)").font(.caption2).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            VStack(alignment: .leading) {
                Text("Talos").font(.headline)
                Text("Open the app and run a check to fill this widget.").font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

@main
struct TalosWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "TalosClusterWidget", provider: Provider()) { entry in
            ClusterWidgetView(entry: entry).containerBackground(.fill.tertiary, for: .widget)
        }
        .configurationDisplayName("Cluster status")
        .description("Nodes ready and etcd alarms from the last check.")
        .supportedFamilies([.systemSmall])
    }
}
