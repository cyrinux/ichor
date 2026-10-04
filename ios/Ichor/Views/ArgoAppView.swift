import SwiftUI
import IchorCore

/// One Argo CD app: the hero with its state and actions, its conditions, the running or last
/// sync, the sync-waves timeline (with resource ticks for a selective sync), the pods that are
/// not ready and the deployment history with rollback. Reads again every 2 s while a sync runs.
struct ArgoAppView: View {
    let namespace: String
    let name: String
    let downNodes: Set<String>

    // Explicit: the private @State properties make the memberwise init private.
    init(namespace: String, name: String, downNodes: Set<String>) {
        self.namespace = namespace
        self.name = name
        self.downNodes = downNodes
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ArgoStatus> = .loading
    @State private var selecting = false
    @State private var selection: Set<String> = []
    /// The sync sheet, for the whole app or the ticked resources.
    @State private var syncSheet: SyncRequest?
    @State private var confirmRollback: ArgoHistory?
    @State private var confirmTerminate = false
    @State private var busy = false
    @State private var message: String?
    @State private var succeeded = 0

    private struct SyncRequest: Identifiable {
        let id = UUID()
        let resources: [ArgoResource]
    }

    private var store: ArgoCDStore { .shared }

    var body: some View {
        LoadStateView(state: state, retry: load) { status in
            if let app = status.app(namespace: namespace, name: name) {
                content(app)
            } else {
                ContentUnavailableView("App not found", systemImage: "questionmark.app",
                                       description: Text("\(name) is no longer an Argo CD Application of this cluster."))
                    .themedBackground()
            }
        }
        .task(id: model.argoKey) {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                if Task.isCancelled { return }
                if store.shouldPoll(for: model.argoKey) { await load() }
            }
        }
        .navigationTitle(Text(verbatim: name))
        .navigationBarTitleDisplayMode(.inline)
        .restartResult($message)
        .sensoryFeedback(.success, trigger: succeeded)
    }

    private func content(_ app: ArgoApp) -> some View {
        List {
            ArgoHero(app: app, busy: busy,
                     sync: { syncSheet = SyncRequest(resources: []) },
                     act: { action in Task { await run(action, on: app) } },
                     terminate: { confirmTerminate = true })
            ArgoConditionsSection(app: app, downNodes: downNodes)
            if let op = app.operation { ArgoOperationSection(operation: op, canTerminate: app.canTerminate) { confirmTerminate = true } }
            if !app.resources.isEmpty { timeline(app) }
            if !app.unhealthyPods.isEmpty { ArgoPodsSection(pods: app.unhealthyPods, downNodes: downNodes) }
            if !app.history.isEmpty { ArgoHistorySection(app: app) { confirmRollback = $0 } }
        }
        .refreshable { await load() }
        .themedBackground()
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if selecting { selectionBar(app) }
        }
        .sheet(item: $syncSheet) { request in
            ArgoSyncSheet(app: app, resources: request.resources) { options in
                await run(.sync, on: app, options: options)
                selecting = false
                selection = []
            }
        }
        .confirmationDialog(confirmRollback.map { String(localized: "Roll \(app.name) back to \($0.label)?") } ?? "",
                            isPresented: Binding(get: { confirmRollback != nil }, set: { if !$0 { confirmRollback = nil } }),
                            titleVisibility: .visible, presenting: confirmRollback) { entry in
            Button("Roll back", role: .destructive) {
                Task { await run(.rollback, on: app, options: ArgoSyncOptions(historyId: entry.id)) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { entry in
            Text("Argo CD syncs \(entry.label) now. Auto-sync stays paused, so the app stays on it until you sync again.")
        }
        .confirmationDialog(String(localized: "Terminate the sync of \(app.name)?"), isPresented: $confirmTerminate,
                            titleVisibility: .visible) {
            Button("Terminate", role: .destructive) { Task { await run(.terminate, on: app) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The resources already applied stay as they are.")
        }
    }

    private func timeline(_ app: ArgoApp) -> some View {
        Section {
            ArgoWaveTimeline(steps: app.waveSteps, running: app.isRunning, selecting: selecting, selection: $selection)
        } header: {
            HStack {
                Text("Sync waves")
                Spacer()
                Button(selecting ? String(localized: "Cancel") : String(localized: "Select")) {
                    withAnimation(.snappy) {
                        selecting.toggle()
                        selection = []
                    }
                }
                .font(.caption.weight(.semibold))
                .textCase(nil)
                .disabled(!app.canSync && !selecting)
            }
        } footer: {
            if selecting { Text("Tick the resources to sync on their own.") }
        }
    }

    private func selectionBar(_ app: ArgoApp) -> some View {
        let chosen = app.resources.filter { selection.contains($0.id) }
        return HStack {
            Text("\(chosen.count) selected").font(.subheadline).monospacedDigit()
            Spacer()
            Button {
                syncSheet = SyncRequest(resources: chosen)
            } label: {
                Label("Sync selected", systemImage: "arrow.triangle.2.circlepath")
            }
            .buttonStyle(.borderedProminent)
            .disabled(chosen.isEmpty || !app.canSync)
        }
        .padding(.horizontal)
        .padding(.vertical, 10)
        .background(.bar)
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = model.argoKey
        if case .loading = state, let known = store.status(for: key) { state = .loaded(known, at: Date()) }
        let loaded: LoadState<ArgoStatus> = await .from { try await store.load(with: client, key: key) }
        guard key == model.argoKey else { return }
        state = state.refreshed(with: loaded)
    }

    private func run(_ action: ArgoAction, on app: ArgoApp, options: ArgoSyncOptions? = nil) async {
        guard let client = model.client else { return }
        // Another request (a refresh, the auto-sync switch) is in flight: wait for it rather
        // than drop this one, which the sync sheet has already dismissed for.
        while busy { try? await Task.sleep(for: .milliseconds(100)) }
        busy = true
        defer { busy = false }
        if let failure = await store.run(action, on: app, options: options, with: client) {
            message = failure
        } else {
            succeeded += 1
        }
        await load()
    }
}
