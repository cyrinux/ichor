import SwiftUI
import IchorCore
import WidgetKit

/// Home-screen summary of the last background check. Counts only, no hostnames: it stays
/// visible when the app lock is on. Looks like the Android widget and the website's mockup.
struct Entry: TimelineEntry {
    let date: Date
    let snapshot: ClusterSnapshot?
}

struct Provider: TimelineProvider {
    private var snapshot: ClusterSnapshot? {
        UserDefaults(suiteName: "group.name.levis.ichor")?.data(forKey: "snapshot")
            .flatMap { try? JSONDecoder().decode(ClusterSnapshot.self, from: $0) }
    }

    func placeholder(in context: Context) -> Entry { Entry(date: .now, snapshot: nil) }
    func getSnapshot(in context: Context, completion: @escaping (Entry) -> Void) { completion(Entry(date: .now, snapshot: snapshot)) }
    func getTimeline(in context: Context, completion: @escaping (Timeline<Entry>) -> Void) {
        let now = Date.now
        let current = snapshot
        var entries = [Entry(date: now, snapshot: current)]
        // A second entry dims the widget when the snapshot turns stale, without waiting for a reload.
        if let current {
            let staleAt = current.takenAt.addingTimeInterval(glanceStaleAfter + 1)
            if staleAt > now { entries.append(Entry(date: staleAt, snapshot: current)) }
        }
        // The app reloads timelines after each check; this is just a fallback refresh.
        completion(Timeline(entries: entries, policy: .after(now.addingTimeInterval(30 * 60))))
    }
}

/// The widget's fixed dark card palette, shared with Android.
enum Glance {
    static let background = rgb(0x18263A)
    static let secondary = rgb(0x93A3B8)
    static let primary = rgb(0xE8EEF5)
    static let ok = rgb(0x5BD18B)
    static let warning = rgb(0xF2C14E)
    static let problem = rgb(0xF0716B)

    static func rgb(_ hex: UInt32) -> Color {
        Color(red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
    }

    static func color(_ tone: GlanceItem.Tone) -> Color {
        switch tone {
        case .ok: return ok
        case .warning: return warning
        case .problem: return problem
        }
    }

    static func text(_ item: GlanceItem) -> Text {
        switch item {
        case .notReady(let n): return Text("\(n) not ready")
        case .unreachable(let n): return Text("\(n) unreachable")
        case .allReady: return Text("all ready")
        case .etcdNoAlarms: return Text("etcd: no alarms")
        case .etcdAlarms(let n): return Text("etcd: \(n) alarms")
        }
    }
}

struct GlanceItemView: View {
    let item: GlanceItem

    var body: some View {
        HStack(spacing: 5) {
            Circle().fill(Glance.color(item.tone)).frame(width: 7, height: 7)
            Glance.text(item).lineLimit(1)
        }
    }
}

struct ClusterWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: Entry

    var body: some View {
        Group {
            if family == .accessoryRectangular {
                lockScreen
            } else {
                card
            }
        }
        // The lock screen draws its own material; the home screen gets the dark card.
        .containerBackground(for: .widget) { family == .accessoryRectangular ? Color.clear : Glance.background }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: entry.snapshot?.context ?? "Talos")
                .font(.system(size: 12))
                .foregroundStyle(Glance.secondary)
                .lineLimit(1)
            count
                .font(.system(size: 24, weight: .semibold))
                .foregroundStyle(Glance.primary)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
                .opacity(dimmed ? 0.5 : 1)
            footer
                .font(.system(size: 12))
                .foregroundStyle(Glance.secondary)
                .padding(.top, 2)
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private var lockScreen: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(verbatim: entry.snapshot?.context ?? "Talos").font(.caption).lineLimit(1)
            count.font(.headline).lineLimit(1)
            if let s = entry.snapshot, !stale(s), let first = glanceItems(s).first {
                GlanceItemView(item: first).font(.caption)
            } else {
                footer.font(.caption).lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var count: Text {
        if let s = entry.snapshot { return Text("\(s.readyCount)/\(s.nodes.count) ready") }
        return Text(verbatim: "–")
    }

    private var dimmed: Bool {
        guard let s = entry.snapshot else { return true }
        return stale(s)
    }

    private func stale(_ s: ClusterSnapshot) -> Bool { isGlanceStale(s, now: entry.date) }

    @ViewBuilder private var footer: some View {
        if let s = entry.snapshot {
            if stale(s) {
                Text("Updated \(s.takenAt, style: .time)").lineLimit(1)
            } else {
                let items = glanceItems(s)
                // Side by side when it fits (medium), stacked otherwise (small).
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 12) { ForEach(items.indices, id: \.self) { GlanceItemView(item: items[$0]) } }
                    VStack(alignment: .leading, spacing: 2) { ForEach(items.indices, id: \.self) { GlanceItemView(item: items[$0]) } }
                }
            }
        } else {
            Text("Open the app to set up").lineLimit(2)
        }
    }
}

@main
struct TalosWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "TalosClusterWidget", provider: Provider()) { entry in
            ClusterWidgetView(entry: entry)
        }
        .configurationDisplayName("Cluster status")
        .description("Nodes ready and etcd alarms from the last check.")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryRectangular])
    }
}
