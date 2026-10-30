import AppIntents
import IchorCore
import SwiftUI

/// "Cluster status" (Shortcuts, Siri): says what the last background check saw on a cluster, from
/// the snapshot the widget shows. No network and no credentials, so it answers without unlocking
/// the app; the names are the widget's (masked in screenshot mode).
struct ClusterStatusIntent: AppIntent {
    static let title: LocalizedStringResource = "Cluster status"
    static let description = IntentDescription(
        "Says what Ichor's last background check saw on a cluster. None chosen: the one open in the app.")

    @Parameter(title: "Cluster")
    var cluster: ClusterEntity?

    init() {}

    func perform() async throws -> some IntentResult & ProvidesDialog & ShowsSnippetView {
        let defaults = UserDefaults(suiteName: SharedSnapshot.suite)
        let chosen = intentCluster(
            cluster?.id, clusters: SharedSnapshot.clusters(from: defaults),
            active: SharedSnapshot.activeCluster(from: defaults))
        let name = chosen?.name ?? cluster?.name ?? String(localized: "Your cluster")
        let summary = clusterStatusSummary(
            SharedSnapshot.load(from: defaults, cluster: chosen?.id ?? cluster?.id), now: .now)
        let sentence = ClusterStatusWords.sentence(summary, name: name, now: .now)
        return .result(dialog: "\(sentence)", view: ClusterStatusSnippet(name: name, summary: summary))
    }
}

/// The parts of a ClusterStatusSummary in the user's language.
enum ClusterStatusWords {
    static func part(_ part: ClusterStatusPart) -> String {
        switch part {
        case .nodes(let n): return String(localized: "\(String(n)) nodes")
        case .allReady: return String(localized: "all ready")
        case .notReady(let n): return String(localized: "\(String(n)) not ready")
        case .unreachable(let n): return String(localized: "\(String(n)) unreachable")
        case .etcdHealthy: return String(localized: "etcd healthy")
        case .etcdAlarms(let n): return String(localized: "etcd: \(String(n)) alarms")
        case .stale: return String(localized: "may be out of date")
        }
    }

    /// "<name>: 8 nodes, all ready, etcd healthy, checked 4 minutes ago".
    static func sentence(_ summary: ClusterStatusSummary?, name: String, now: Date) -> String {
        guard let summary else { return name + ": " + String(localized: "no background check yet") }
        let age = RelativeDateTimeFormatter().localizedString(for: summary.checkedAt, relativeTo: now)
        let words = summary.parts.map(part) + [String(localized: "checked \(age)")]
        return name + ": " + words.joined(separator: ", ")
    }
}

/// What Siri and Shortcuts show under the sentence: the cluster and its parts as dots.
struct ClusterStatusSnippet: View {
    let name: String
    let summary: ClusterStatusSummary?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(name).font(.headline)
            if let summary {
                ForEach(Array(summary.parts.enumerated()), id: \.offset) { _, part in
                    HStack(spacing: 6) {
                        Circle().fill(color(part)).frame(width: 8, height: 8)
                        Text(ClusterStatusWords.part(part))
                    }
                }
                Text(summary.checkedAt, style: .relative).font(.caption).foregroundStyle(.secondary)
            } else {
                Text("no background check yet").foregroundStyle(.secondary)
            }
        }
        .padding()
    }

    private func color(_ part: ClusterStatusPart) -> Color {
        switch part {
        case .notReady, .stale: return .orange
        case .unreachable, .etcdAlarms: return .red
        case .nodes: return .secondary
        case .allReady, .etcdHealthy: return .green
        }
    }
}

/// What ClusterScreenHandler does in the app: the cluster's own share link (the one a widget set
/// to it opens), turned into the checkup's when asked; the app opens it once unlocked.
enum ClusterScreenLinks {
    @MainActor
    static func install() {
        ClusterScreenHandler.open = { id, screen in await open(id, screen) }
    }

    @MainActor
    private static func open(_ id: String?, _ screen: ShareTarget.Target) async {
        let defaults = UserDefaults(suiteName: SharedSnapshot.suite)
        guard
            let cluster = intentCluster(
                id, clusters: SharedSnapshot.clusters(from: defaults),
                active: SharedSnapshot.activeCluster(from: defaults)),
            let link = cluster.link.flatMap(URL.init(string:))
        else { return }
        guard screen != .cluster else {
            NotificationRouter.shared.pendingShareLink = link
            return
        }
        guard var target = try? await TalosClient.parseShareLink(link) else { return }
        target.target = screen
        if let other = TalosClient.shareLink(for: target) { NotificationRouter.shared.pendingShareLink = other }
    }
}
