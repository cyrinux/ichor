import SwiftUI
import IchorCore

/// Upgrade cluster: the version, every node in the order the roll follows with its checks, and
/// one Start (Continue after an interrupted roll); then the followed roll (ClusterUpgradeRunView),
/// which goes on when leaving the screen.
struct ClusterUpgradePlanView: View {
    /// The version the overview offered ("": the newest stable release).
    let initialVersion: String

    @Environment(AppModel.self) private var model
    @State private var releases: [TalosRelease] = []
    @State private var releasesError: String?
    @State private var version = ""
    @State private var plan: LoadState<ClusterUpgradePlan>?
    @State private var drain = true
    @State private var confirming = false
    @State private var message: String?

    private var job: ClusterUpgradeJob { .shared }
    private var cluster: String { model.activeContext }

    var body: some View {
        Group {
            if job.version != nil {
                ClusterUpgradeRunView(job: job) {
                    job.clear()
                    Task { await loadPlan() }
                }
            } else {
                planList
            }
        }
        .navigationTitle("Upgrade cluster")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .sheet(isPresented: $confirming) {
            if case .loaded(let p, _, _)? = plan {
                HostnameConfirmationSheet(
                    title: String(localized: "Upgrade \(cluster) to \(p.version)?"),
                    message: String(localized: "Every node reboots in turn. Pods move between nodes as they drain; a single-replica workload stops while its node reboots."),
                    hostname: cluster,
                    actionTitle: String(localized: "Upgrade every node"),
                    acknowledgments: p.allWarnings
                ) {
                    confirming = false
                    Task { await start(p) }
                }
            }
        }
    }

    private var planList: some View {
        List {
            if model.activeSummary?.demo == true {
                Section { Text("Demo cluster: the plan is a sample, and the upgrade is refused.").note() }
            }
            Section {
                Text("Every node is upgraded one after the other: control planes first, the etcd leader last of them, then the workers. Between nodes, the app checks the cluster is healthy again.")
                    .font(.footnote).foregroundStyle(.secondary)
                Picker("Version", selection: $version) {
                    ForEach(offeredVersions, id: \.self) { Text(verbatim: $0).tag($0) }
                }
                .onChange(of: version) { _, _ in Task { await loadPlan() } }
                if let releasesError { Text(verbatim: releasesError).font(.footnote).foregroundStyle(.secondary) }
            }
            if let plan {
                switch plan {
                case .loading:
                    Section { ProgressView().frame(maxWidth: .infinity) }
                case .failed(let error):
                    Section {
                        Text(verbatim: error).foregroundStyle(.statusBad)
                        Button("Retry") { Task { await loadPlan() } }
                    }
                case .loaded(let p, _, _):
                    planSections(p)
                }
            }
        }
        .themedBackground()
    }

    @ViewBuilder private func planSections(_ p: ClusterUpgradePlan) -> some View {
        if !p.drain {
            Section {
                Toggle("Drain each node first", isOn: $drain)
            } footer: {
                Text("Cordon the node and evict its pods before its upgrade, so they move before it reboots.")
            }
        }
        Section {
            ForEach(Array(p.nodes.enumerated()), id: \.element.id) { index, node in
                nodeRow(index + 1, node)
            }
        } header: {
            Text("Nodes, in upgrade order (\(p.done) of \(p.nodes.count) done)")
        }
        Section("Checks") {
            ForEach(p.allBlockers, id: \.self) { blocker in
                Label { Text(verbatim: blocker) } icon: { Image(systemName: "xmark.octagon.fill") }.foregroundStyle(.statusBad)
            }
            ForEach(p.allWarnings, id: \.self) { warning in
                Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }.foregroundStyle(.statusWarn)
            }
            if p.allBlockers.isEmpty && p.allWarnings.isEmpty { Text("No issues found").foregroundStyle(.statusOK) }
        }
        Section {
            if p.upToDate { Text("Every node already runs \(p.version).").foregroundStyle(.statusOK) }
            if let busy { Text(verbatim: busy).font(.footnote).foregroundStyle(.statusWarn) }
            Button {
                confirming = true
            } label: {
                Text(p.continues ? "Continue the upgrade" : "Upgrade every node").fontWeight(.semibold).frame(maxWidth: .infinity)
            }
            .disabled(!p.canStart || busy != nil)
            if let message { Text(verbatim: message).font(.footnote).foregroundStyle(.statusBad) }
        } footer: {
            Text("Keep Ichor open until the end: if the app stops, start again and it continues with the nodes left.")
        }
    }

    private func nodeRow(_ position: Int, _ node: ClusterUpgradeNode) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: "\(position). \(node.name)")
                Text(verbatim: [node.controlPlane ? String(localized: "Control plane") : String(localized: "Worker"),
                                node.leader ? String(localized: "etcd leader") : "", node.from]
                    .filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if node.nodeState == .done {
                StatusPill(label: String(localized: "On the version"), color: .statusOK)
            } else if !node.blockers.isEmpty {
                StatusPill(label: String(localized: "Blocked"), color: .statusBad)
            } else if !node.warnings.isEmpty {
                StatusPill(label: String(localized: "Warning"), color: .statusWarn)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// Another node upgrade or maintenance runs: one thing at a time on the cluster.
    private var busy: String? {
        if UpgradeJob.shared.isActive { return String(localized: "Another upgrade is running: wait for it to end.") }
        if MaintenanceJob.shared.isActive { return String(localized: "A node maintenance is running: wait for it to end.") }
        return nil
    }

    private var offeredVersions: [String] {
        var all = releases.filter { !$0.prerelease }.map(\.version)
        if !version.isEmpty, !all.contains(version) { all.insert(version, at: 0) }
        return Array(all.prefix(6))
    }

    private func load() async {
        if version.isEmpty { version = initialVersion }
        do {
            releases = try await TalosClient.talosReleases()
            if version.isEmpty { version = releases.first { !$0.prerelease }?.version ?? "" }
        } catch {
            releasesError = error.localizedDescription
        }
        if plan == nil { await loadPlan() }
    }

    private func loadPlan() async {
        guard let client = model.client, !version.isEmpty else { return }
        plan = .loading
        let picked = version
        let loaded = await LoadState.from { try await client.clusterUpgradePlan(version: picked) }
        if picked == version { plan = loaded }
    }

    /// Like an upgrade: a fresh Face ID / passcode first when the app lock is on.
    private func start(_ p: ClusterUpgradePlan) async {
        message = nil
        if model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Upgrade the cluster \(cluster)")) {
            message = failure
            return
        }
        guard let client = model.client else { return }
        job.start(client: client, version: p.version, drain: drain && !p.drain, acknowledged: true)
    }
}
