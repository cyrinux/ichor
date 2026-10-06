import SwiftUI
import IchorCore

/// "Talos v1.14.2 is available · 8 nodes on older versions" at the top of the overview.
/// Admins open the rollout (TalosRolloutView); other roles get the info only.
struct TalosUpdateSection: View {
    let info: TalosUpdateInfo
    let nodes: [NodeOverview]

    @Environment(AppModel.self) private var model

    private var outdated: Int {
        nodes.filter { $0.reachable && isOutdatedTalos($0.version, latest: info.latest) }.count
    }

    // Not tied to the local count: the rollout stays open while the last outdated node reboots.
    private var canUpgrade: Bool { model.allows(.upgrade) }

    var body: some View {
        if let count = talosUpdateBannerCount(info, localOutdated: outdated) {
            Section {
                if canUpgrade {
                    NavigationLink {
                        TalosRolloutView(latest: info.latest, nodes: nodes)
                    } label: {
                        bannerLabel(count: count)
                    }
                } else {
                    bannerLabel(count: count)
                }
                if info.notes.hasPrefix("https://"), let url = URL(string: info.notes) {
                    Link(destination: url) {
                        Label("Release notes", systemImage: "doc.text")
                    }
                }
            }
        }
    }

    private func bannerLabel(count: Int) -> some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text("Talos \(info.latest) is available") + Text(verbatim: " · ") + Text("\(count) nodes on older versions")
                if canUpgrade {
                    Text("Tap to upgrade a node.").font(.caption).foregroundStyle(.secondary)
                }
            }
        } icon: {
            Image(systemName: "arrow.up.circle.fill")
        }
        .foregroundStyle(Color.blue)
    }
}

/// The rolling upgrade to `latest` as a plan to come back to: every node under its role in
/// upgrade order with where it stands, what holds the rollout up, and one button for the next
/// node. The rows are the expert path; a worker ahead of the control plane asks first.
private struct TalosRolloutView: View {
    let latest: String
    let nodes: [NodeOverview]

    @Environment(AppModel.self) private var model
    /// nil when it cannot be read: the upgrade screen's own etcd checks still apply.
    @State private var etcd: EtcdHealth?
    @State private var opened: NodeOverview?
    /// A worker picked ahead of the control plane, to confirm.
    @State private var anyway: RolloutRow?

    private var job: UpgradeJob { .shared }

    /// The followed upgrade; none once the user stopped following (its result is unknown).
    private var run: RolloutRun? {
        guard let target = job.target else { return nil }
        switch job.outcome {
        case .unfollowed?: return nil
        case .succeeded(let newVersion)?: return RolloutRun(node: target.node, waiting: true, finished: true, reached: newVersion)
        case .failed?: return RolloutRun(node: target.node, finished: true, failed: true)
        case nil: return RolloutRun(node: target.node, waiting: upgradeWaitsForNode(job.events))
        }
    }

    /// Changes with a node's health or version: etcd is read again then.
    private var healthKey: [String] { nodes.map { "\($0.node) \($0.reachable) \($0.ready) \($0.version)" } }

    var body: some View {
        let plan = TalosRollout(nodes: nodes, latest: latest, run: run, etcd: etcd)
        List {
            if let hold = plan.hold {
                Section { holdRow(hold, etcd: plan.etcd) }
            }
            if let next = plan.next {
                Section {
                    Button { pick(next, in: plan) } label: {
                        Label("Upgrade next: \(next.node.hostname)", systemImage: "arrow.up.circle.fill").fontWeight(.semibold)
                    }
                    .disabled(!plan.canOpen(next))
                }
            }
            section(plan.controlPlane, in: plan, later: false) {
                Text("Control plane · \(plan.controlPlaneDone)/\(plan.controlPlane.count) done")
            }
            section(plan.workers, in: plan, later: plan.workersWait) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Workers · \(plan.workersDone)/\(plan.workers.count) done")
                    // A dimmed row with no reason looks broken: say why, right above them.
                    if plan.workersWait { Text("Available after the control plane.").textCase(nil) }
                }
            }
        }
        .themedBackground()
        .navigationTitle(String(localized: "Upgrade which node to \(latest)?"))
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(item: $opened) { node in
            UpgradeView(node: node.node, hostname: node.hostname, initialVersion: latest)
        }
        .task(id: healthKey) {
            // Asked again while a member is unhealthy: it recovers without any node changing in the overview.
            repeat {
                etcd = (try? await model.client?.etcd())?.health
                guard etcd?.degraded == true else { break }
                try? await Task.sleep(for: .seconds(5))
            } while !Task.isCancelled
        }
        .alert(
            Text("Upgrade \(anyway?.node.hostname ?? "") before the control plane?"),
            isPresented: Binding(get: { anyway != nil }, set: { if !$0 { anyway = nil } }),
            presenting: anyway
        ) { row in
            Button("Upgrade anyway") { open(row) }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("Control-plane nodes normally go first, and only \(plan.controlPlaneDone) of \(plan.controlPlane.count) are done. For a patch release the order is harmless.")
        }
    }

    private func pick(_ row: RolloutRow, in plan: TalosRollout) {
        if plan.needsConfirm(row) { anyway = row } else { open(row) }
    }

    private func open(_ row: RolloutRow) {
        // A failed run is forgotten, so that the upgrade screen plans a new one instead of showing it.
        if row.state == .failed { job.clear() }
        opened = row.node
    }

    /// Why nothing else can start now, at the top of the list.
    @ViewBuilder
    private func holdRow(_ hold: RolloutHold, etcd: EtcdHealth?) -> some View {
        switch hold {
        case .upgrading(let node, let waiting):
            HStack(spacing: 10) {
                ProgressView()
                VStack(alignment: .leading, spacing: 2) {
                    if waiting {
                        Text("\(node.hostname) is upgraded. Waiting for it to be healthy…")
                    } else {
                        Text("Upgrading \(node.hostname)…")
                    }
                    // Only a control-plane node takes an etcd member down with it.
                    if let etcd, node.role == "controlplane" {
                        Text("etcd \(etcd.healthy)/\(etcd.members)").font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                }
            }
        case .unhealthy(let nodes):
            if let node = nodes.first {
                Group {
                    if node.reachable {
                        Text("\(node.hostname) is not ready. Wait for it before upgrading another node.")
                    } else {
                        Text("\(node.hostname) is unreachable. Wait for it before upgrading another node.")
                    }
                }
                .foregroundStyle(.statusBad)
            }
        case .etcd(let health):
            Text("etcd has \(health.healthy) of \(health.members) members healthy. Fix it before upgrading a node.")
                .foregroundStyle(.statusBad)
        }
    }

    @ViewBuilder
    private func section<Header: View>(_ rows: [RolloutRow], in plan: TalosRollout, later: Bool,
                                       @ViewBuilder header: () -> Header) -> some View {
        if !rows.isEmpty {
            let title = header()
            Section {
                ForEach(rows) { row in nodeRow(row, in: plan, later: later) }
            } header: {
                title
            }
        }
    }

    private func nodeRow(_ row: RolloutRow, in plan: TalosRollout, later: Bool) -> some View {
        let enabled = plan.canOpen(row)
        return Button { pick(row, in: plan) } label: {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: row.node.hostname).font(.headline)
                    Text(verbatim: [row.node.version, row.node.node].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }
                Spacer()
                stateMark(row.state, enabled: enabled)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .opacity(opacity(row, enabled: enabled, later: later))
    }

    /// Full for the node being upgraded; a held row like a disabled control; a done one, or one
    /// better left for later, less prominent yet clearly not disabled.
    private func opacity(_ row: RolloutRow, enabled: Bool, later: Bool) -> Double {
        if row.state == .upgrading || row.state == .waitingHealthy { return 1 }
        if !enabled && !row.done { return 0.38 }
        return row.done || later ? 0.6 : 1
    }

    /// Where the node stands, at the end of its row; a chevron marks a pending one that opens.
    @ViewBuilder
    private func stateMark(_ state: RolloutState, enabled: Bool) -> some View {
        switch state {
        case .pending:
            if enabled {
                Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
            }
        case .upgrading:
            ProgressView()
            Text("Upgrading").font(.caption)
        case .waitingHealthy:
            ProgressView()
            Text("Waiting for healthy").font(.caption)
        case .done:
            Image(systemName: "checkmark.circle").foregroundStyle(.statusOK).accessibilityLabel(Text("Done"))
        case .failed:
            Group {
                if enabled { Text("Failed · Retry") } else { Text("Failed") }
            }
            .font(.caption)
            .foregroundStyle(.statusBad)
        }
    }
}
