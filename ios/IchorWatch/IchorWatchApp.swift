import IchorCore
import SwiftUI

/// The Apple Watch app (CYR-339): the clusters and their nodes from the iPhone's last
/// background check, read-only.
@main
struct IchorWatchApp: App {
    @WKApplicationDelegateAdaptor(WatchAppDelegate.self) private var delegate
    @StateObject private var model = WatchModel.shared

    var body: some Scene {
        WindowGroup {
            ClusterListView(payload: model.payload)
                .onAppear { model.activate() }
        }
    }
}

struct ClusterListView: View {
    let payload: WatchPayload?

    var body: some View {
        NavigationStack {
            Group {
                if let payload, !payload.clusters.isEmpty {
                    List {
                        ForEach(payload.clusters) { cluster in
                            NavigationLink(value: cluster.id) { ClusterRow(cluster: cluster) }
                        }
                        Section { LastCheckFooter(clusters: payload.clusters) }
                    }
                } else {
                    ScrollView {
                        Text("Open Ichor on your iPhone and turn on background checks.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                            .padding()
                    }
                }
            }
            .navigationTitle(Text("Clusters"))
            .navigationDestination(for: String.self) { id in
                if let cluster = payload?.clusters.first(where: { $0.id == id }) { NodesView(cluster: cluster) }
            }
        }
    }
}

/// A cluster: its colour, its name and the widget's status items (or its age when stale).
struct ClusterRow: View {
    let cluster: WatchCluster

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle()
                .fill(WatchStyle.color(cluster.tone))
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: cluster.name).font(.headline).lineLimit(1)
                TimelineView(.everyMinute) { context in
                    status(now: context.date).font(.caption2).foregroundStyle(.secondary)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private func status(now: Date) -> some View {
        if let takenAt = cluster.takenAt {
            if cluster.isStale(now: now) {
                Text("Updated \(takenAt, style: .time)").lineLimit(1)
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(cluster.glance.indices, id: \.self) { WatchStyle.text(cluster.glance[$0]).lineLimit(1) }
                }
            }
        } else {
            Text("No check yet").lineLimit(1)
        }
    }
}

/// One cluster's nodes, the ones down first, with their state and reason.
struct NodesView: View {
    let cluster: WatchCluster

    var body: some View {
        List {
            if cluster.takenAt != nil {
                Section {
                    WatchStyle.ready(cluster).font(.headline)
                }
            }
            ForEach(cluster.nodes.indices, id: \.self) { index in
                NodeRow(node: cluster.nodes[index], index: index)
            }
            Section { LastCheckFooter(clusters: [cluster]) }
        }
        .navigationTitle(Text(verbatim: cluster.name))
    }
}

struct NodeRow: View {
    let node: WatchNode
    let index: Int

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle()
                .fill(WatchStyle.color(node.health))
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                WatchStyle.name(node, index: index).font(.body).lineLimit(1)
                WatchStyle.state(node.health).font(.caption2).foregroundStyle(WatchStyle.color(node.health))
                if let problem = node.problem.flatMap(WatchStyle.problem) {
                    problem.font(.caption2).foregroundStyle(.secondary).lineLimit(2)
                } else if !node.reason.isEmpty {
                    Text(verbatim: node.reason).font(.caption2).foregroundStyle(.secondary).lineLimit(4)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// "Last check 5 minutes ago" (the newest of `clusters`), and a hint to open the phone when stale.
struct LastCheckFooter: View {
    let clusters: [WatchCluster]

    var body: some View {
        TimelineView(.everyMinute) { context in
            VStack(alignment: .leading, spacing: 4) {
                if let last = clusters.compactMap(\.takenAt).max() {
                    Text("Last check \(Text(last, format: .relative(presentation: .named)))")
                    if context.date.timeIntervalSince(last) > glanceStaleAfter {
                        Text("Open Ichor on your iPhone to refresh.")
                    }
                } else {
                    Text("Open Ichor on your iPhone and turn on background checks.")
                }
            }
            .font(.footnote)
            .foregroundStyle(.secondary)
        }
        .listRowBackground(Color.clear)
    }
}

#Preview {
    let now = Date()
    let nodes = [WatchNode(name: "worker-2", health: .unreachable, reason: "i/o timeout"),
                 WatchNode(name: "cp-1", health: .ready), WatchNode(name: "worker-1", health: .ready)]
    return ClusterListView(payload: WatchPayload(sentAt: now, masked: false, clusters: [
        WatchCluster(id: "0", name: "Lab", active: true, takenAt: now, nodes: nodes, etcdChecked: true),
        WatchCluster(id: "1", name: "Edge", active: false),
    ]))
}
