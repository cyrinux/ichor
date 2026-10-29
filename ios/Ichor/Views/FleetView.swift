import IchorCore
import SwiftUI

/// Every cluster's health on one list, worst first (the fleet view): the last background check
/// of each (SharedStore), ranked by the Go core. Opening it calls no cluster; "Check every
/// cluster now" runs one check. A tap switches to that cluster and goes back to its home; on the
/// demo's made-up fleet it only goes back. With alerts off it offers their settings, since
/// iOS then never checks in the background. Same content and order as Android's FleetScreen.
struct FleetView: View {
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @State private var summary: FleetSummary?
    @State private var failed = false
    @State private var refreshing = false
    @State private var now = Date()
    private let alertsOn = BackgroundMonitor.alertsEnabled

    /// What the list is read again for: a rename, screenshot mode, a cluster added or removed.
    private struct Inputs: Equatable {
        let labels: ClusterLabels
        let contexts: [String]
    }

    var body: some View {
        let contexts = model.summary?.contexts ?? []
        let demo = demoFleetOnly(contexts)
        List {
            if let summary, !summary.clusters.isEmpty {
                Section {
                    Text(verbatim: summaryLine(summary.counts)).font(.footnote).foregroundStyle(.secondary)
                    if !alertsOn && !demo { alertsOff }
                    if demo {
                        Text("The demo's made-up clusters: a tap goes back to the demo.")
                            .font(.footnote).foregroundStyle(.secondary)
                    }
                }
                Section {
                    ForEach(summary.clusters) { row in
                        let cluster = contexts.first { $0.name == row.context && !$0.demo }
                        Button {
                            open(cluster)
                        } label: {
                            FleetRowView(
                                row: row, seed: cluster.map { model.seed(of: $0) }, masked: model.labels.masked,
                                now: now)
                        }
                        .foregroundStyle(.primary)
                    }
                } footer: {
                    if summary.clusters.count == 1 { Text("The list grows with each cluster you add.") }
                }
            }
        }
        .overlay { emptyState }
        .navigationTitle("All clusters")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !demo {
                ToolbarItem(placement: .primaryAction) {
                    if refreshing {
                        ProgressView()
                    } else {
                        Button {
                            Task { await refresh() }
                        } label: {
                            Label("Check every cluster now", systemImage: "arrow.clockwise")
                        }
                    }
                }
            }
        }
        // Read again on open, on each change of names or clusters, and once a minute (the ages).
        .task(id: Inputs(labels: model.labels, contexts: contexts.map(\.name))) {
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    @ViewBuilder private var emptyState: some View {
        if failed {
            ContentUnavailableView(
                "The list could not be built from the last checks.", systemImage: "exclamationmark.triangle")
        } else if summary?.clusters.isEmpty == true {
            ContentUnavailableView("No cluster yet.", systemImage: "square.stack.3d.up")
        }
    }

    private var alertsOff: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(
                "Background checks are off, so no cluster is checked and this list stays as it is. Turn on alerts to fill it."
            )
            .font(.footnote)
            Button("Alerts settings") { path.append(.settingsPage(.alerts)) }
        }
    }

    /// "Need attention: 2 · Not checked: 1 · Fine: 5", without the zeros.
    private func summaryLine(_ counts: FleetCounts) -> String {
        [
            counts.needAttention > 0 ? String(localized: "Need attention: \(counts.needAttention)") : nil,
            counts.unknown > 0 ? String(localized: "Not checked: \(counts.unknown)") : nil,
            counts.ok > 0 ? String(localized: "Fine: \(counts.ok)") : nil,
        ].compactMap { $0 }.joined(separator: " · ")
    }

    /// The cluster on screen becomes `cluster` (nil: a demo row, nothing to switch to), then its home.
    private func open(_ cluster: ContextSummary?) {
        path = []
        if let cluster, cluster.name != model.activeContext { model.activeContext = cluster.name }
    }

    private func load() async {
        let contexts = model.summary?.contexts ?? []
        let at = Date()
        do {
            if demoFleetOnly(contexts) {
                summary = try await TalosClient.demoFleetSummary(now: at)
            } else {
                let entries = fleetEntries(
                    snapshots: SharedStore.snapshots(activeCluster: model.activeSummary.map(monitorClusterKey)),
                    reachability: BackgroundMonitor.reachability(), contexts: contexts, labels: model.labels)
                summary = try await TalosClient.fleetSummary(entries: entries, now: at)
            }
            failed = false
        } catch {
            failed = summary == nil
        }
        now = at
    }

    private func refresh() async {
        refreshing = true
        await BackgroundMonitor.check()
        await load()
        refreshing = false
    }
}
