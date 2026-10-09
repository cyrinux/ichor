import SwiftUI
import IchorCore

/// The Argo CD Applications deploying an inventory app (os:admin, clusters running Argo CD):
/// health, sync, revision, a sync in progress, Sync and Refresh, and their screen. Argo CD's own
/// tile gets the GitOps summary instead. Uses what the Overview or the Argo CD screen already
/// loaded, else loads once when the sheet opens. Nothing when no Application matches.
struct AppArgoSection: View {
    let app: InventoryApp

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ArgoStatus> = .loading
    @State private var confirmSync: ArgoApp?
    @State private var busy: Set<String> = []
    @State private var message: String?
    @State private var succeeded = 0

    private var store: ArgoCDStore { .shared }

    var body: some View {
        content
            .task(id: app) {
                if let known = store.status(for: model.argoKey) {
                    state = .loaded(known, at: Date())
                } else {
                    state = .loading
                    await load()
                }
            }
            .confirmationDialog(confirmSync.map { String(localized: "Sync \($0.name)?") } ?? "",
                                isPresented: $confirmSync.isPresent(),
                                titleVisibility: .visible, presenting: confirmSync) { target in
                Button("Sync") { Task { await run(.sync, on: target, options: ArgoSyncOptions(defaultsFor: target)) } }
                Button("Cancel", role: .cancel) {}
            } message: { target in
                Text("\(target.name) syncs to \(target.sources.first?.targetRevision ?? target.revision) with its own sync options. Nothing is pruned.")
            }
            .messageAlert($message)
            .sensoryFeedback(.success, trigger: succeeded)
            .loadsKubeActionAccess(namespace: appsNamespace)
    }

    /// The namespace of the Applications shown when they share one.
    private var appsNamespace: String {
        guard case .loaded(let status, _, _) = state, app.id != argoCDCatalogID else { return "" }
        return kubeSharedNamespace(argoApps(for: app, in: status).map(\.namespace))
    }

    @ViewBuilder private var content: some View {
        switch state {
        case .loading:
            Section { Text("Reading Argo CD…").note() } header: { Text(verbatim: "Argo CD") }
        case .failed(let error):
            Section { Text("Could not read: \(error)").note() } header: { Text(verbatim: "Argo CD") }
        case .loaded(let status, _, _):
            if app.id == argoCDCatalogID {
                summary(status)
            } else {
                let matched = argoApps(for: app, in: status)
                if !matched.isEmpty {
                    Section {
                        ForEach(matched) { line($0) }
                    } header: {
                        Text(verbatim: "Argo CD")
                    }
                }
            }
        }
    }

    private func summary(_ status: ArgoStatus) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("\(status.apps.count) apps").font(.subheadline.weight(.semibold)).monospacedDigit()
                    if !status.version.isEmpty { InfoChip(text: status.version, monospaced: true) }
                    Spacer()
                    if status.worst.needsAttention { StatusPill(label: status.worst.label, color: status.worst.color) }
                }
                ArgoHealthBar(counts: status.healthCounts)
                Text(verbatim: ServiceHealth.allCases.compactMap { level in
                    let count = status.apps.filter { $0.level == level }.count
                    return count > 0 ? "\(count) \(level.label)" : nil
                }.joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
            ForEach(status.running) { running in
                HStack(spacing: 8) {
                    Image(systemName: "arrow.triangle.2.circlepath").foregroundStyle(.blue).symbolEffect(.pulse)
                    Text(verbatim: running.name).font(.callout)
                    Spacer()
                    if let op = running.operation {
                        Text(verbatim: argoProgressText(op)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
            }
            NavigationLink {
                ArgoCDView(downNodes: [])
            } label: {
                Label("Open Argo CD apps", systemImage: "arrow.triangle.branch")
            }
        } header: {
            Text(verbatim: "GitOps")
        }
    }

    private func line(_ argo: ArgoApp) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            NavigationLink {
                ArgoAppView(namespace: argo.namespace, name: argo.name, downNodes: [])
            } label: {
                HStack(spacing: 10) {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(verbatim: argo.name).font(.callout.weight(.medium)).lineLimit(1)
                            if let owner = argo.owner { ArgoOwnerBadge(owner: owner) }
                        }
                        Text(verbatim: [argo.revisionLabel, argo.deployedAt > 0 ? relativeTime(argo.deployedAt) : ""]
                            .filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                        if let op = argo.operation, op.phase.active {
                            Text(verbatim: [op.phase.label, argoProgressText(op)].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.caption.weight(.medium))
                                .foregroundStyle(.blue)
                        }
                        // A hotfix is made here: say Argo CD holds off, and until when (freezing is on the app page).
                        if let freeze = argo.freeze {
                            Label(String(localized: "Frozen until \(freezeClock(freeze.until))"), systemImage: "snowflake")
                                .font(.caption)
                                .foregroundStyle(.blue)
                        }
                    }
                    Spacer(minLength: 4)
                    HStack(spacing: 6) {
                        StatusPill(label: argo.health.label, color: argo.health.color)
                        Image(systemName: argo.sync.symbol).foregroundStyle(argo.sync.color)
                            .accessibilityLabel(Text(argo.sync.label))
                    }
                }
            }
            HStack(spacing: 10) {
                Button { confirmSync = argo } label: { Label("Sync", systemImage: "arrow.triangle.2.circlepath") }
                    .buttonStyle(.bordered)
                    .disabled(!argo.canSync || busy.contains(argo.id))
                Button { Task { await run(.refresh, on: argo) } } label: { Label("Refresh", systemImage: "arrow.clockwise") }
                    .buttonStyle(.bordered)
                    .disabled(busy.contains(argo.id))
                if busy.contains(argo.id) { ProgressView() }
            }
            .controlSize(.small)
            .kubeGated(.argoSync, in: argo.namespace)
            KubeDeniedNote(.argoSync, in: argo.namespace)
        }
        .padding(.vertical, 2)
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = model.argoKey
        let cluster = model.activeSummary, store = self.store
        await store.refresh($state, key: key, seed: false, currentKey: { model.argoKey }) {
            try await store.load(with: client, key: key, cluster: cluster)
        }
    }

    private func run(_ action: ArgoAction, on argo: ArgoApp, options: ArgoSyncOptions? = nil) async {
        guard let client = model.client, !busy.contains(argo.id) else { return }
        busy.insert(argo.id)
        defer { busy.remove(argo.id) }
        let failure = await store.run(action, on: argo, options: options, with: client)
        if recordActionOutcome(failure, message: &message, succeeded: &succeeded) { announce(String(localized: "Done")) }
        await load()
    }
}

/// The Apps grid's mark on a tile whose Argo CD Application is broken or drifting.
struct ArgoTileBadge: View {
    // Grows with Dynamic Type, like the caption it sits next to.
    @ScaledMetric(relativeTo: .caption2) private var diameter: CGFloat = 18

    var body: some View {
        Image(systemName: "arrow.triangle.branch")
            .font(.caption2.weight(.bold))
            .imageScale(.small)
            .foregroundStyle(.white)
            .frame(width: diameter, height: diameter)
            .background(Circle().fill(attentionColor))
            .overlay { Circle().stroke(Color(.secondarySystemGroupedBackground), lineWidth: 2) }
            .accessibilityLabel(Text("Argo CD app needs a look"))
    }
}
