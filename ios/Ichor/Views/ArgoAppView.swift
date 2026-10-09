import SwiftUI
import IchorCore

/// One Argo CD app: the hero with its state and actions, its conditions, the running or last
/// sync, how traffic reaches it (the network graph), the sync-waves timeline (with resource ticks for a selective sync), the pods that are
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
    @State private var network = ArgoNetworkModel()
    /// The node a box of the network graph opens.
    @State private var openNode: NodeRef?
    @State private var freezeSheet = false
    @State private var confirmUnfreeze = false

    private struct SyncRequest: Identifiable {
        let id = UUID()
        let resources: [ArgoResource]
    }

    private var store: ArgoCDStore { .shared }

    var body: some View {
        LoadStateView(state: state, retry: load) { status in
            if let app = status.app(namespace: namespace, name: name) {
                content(app, status: status)
            } else {
                ContentUnavailableView("App not found", systemImage: "questionmark.app",
                                       description: Text("\(name) is no longer an Argo CD Application of this cluster."))
                    .themedBackground()
            }
        }
        .gitOpsPolling(key: model.argoKey, shouldPoll: { store.shouldPoll(for: model.argoKey) }, load: { await load() })
        .navigationTitle(Text(verbatim: name))
        .toolbar { ToolbarItem(placement: .primaryAction) { ShareLinkButton(target: .argoApp(namespace: namespace, name: name)) } }
        .navigationBarTitleDisplayMode(.inline)
        .messageAlert($message)
        .sensoryFeedback(.success, trigger: succeeded)
        .navigationDestination(item: $openNode) { NodeDetailView(ref: $0) }
        .navigationDestination(for: ArgoWindowsRoute.self) { _ in ArgoWindowsView() }
    }

    private func content(_ app: ArgoApp, status: ArgoStatus) -> some View {
        let project = status.project(of: app)
        let windows = status.freezeWindows(of: app).map(\.window)
        let ichorWindows = windows.filter { $0.ichor != nil }
        return List {
            ArgoHero(app: app, busy: busy,
                     sync: { syncSheet = SyncRequest(resources: []) },
                     act: { action in Task { await run(action, on: app) } },
                     terminate: { confirmTerminate = true })
            Section {
                NavigationLink { FluxDiffView(argo: app.namespace, name: app.name) } label: {
                    Label("Show diff", systemImage: "plus.forwardslash.minus")
                }
            } footer: {
                if app.sync == .outOfSync {
                    Text("The app is out of sync: see what a sync would change before running it.")
                } else {
                    Text("What a sync would change now, compared in the cluster without changing anything.")
                }
            }
            ArgoFreezeSection(app: app, windows: windows, busy: busy || project.map { store.busyProjects.contains($0.id) } == true,
                              freeze: { freezeSheet = true },
                              extend: {
                                  // The window ending last is the one "until" shows.
                                  guard let project, let window = ichorWindows.max(by: { $0.endsAt < $1.endsAt }) else { return }
                                  Task { await freeze(.extend, on: project, options: [ArgoFreezeOptions(minutes: freezeExtendMinutes, window: window.id)]) }
                              },
                              unfreeze: { confirmUnfreeze = true })
            ArgoConditionsSection(app: app, downNodes: downNodes)
            if let op = app.operation { ArgoOperationSection(operation: op, canTerminate: app.canTerminate) { confirmTerminate = true } }
            ArgoNetworkSection(model: network, downNodes: downNodes, pods: app.unhealthyPods,
                               openNode: { openNode = $0 }, changed: { Task { await load() } })
            if !app.resources.isEmpty { timeline(app) }
            if !app.unhealthyPods.isEmpty { ArgoPodsSection(pods: app.unhealthyPods, downNodes: downNodes) }
            if !app.history.isEmpty { ArgoHistorySection(app: app) { confirmRollback = $0 } }
        }
        .refreshable { await load() }
        .themedBackground()
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if selecting { selectionBar(app) }
        }
        .sheet(isPresented: $freezeSheet) {
            ArgoFreezeSheet(app: app, status: status,
                            freeze: { project, options in await freeze(.freeze, on: project, options: [options]) },
                            pauseInstead: app.canChangeSpec && app.autoSync.enabled ? { await run(.autoSyncOff, on: app) } : nil)
        }
        .confirmationDialog(String(localized: "End the freeze?"), isPresented: $confirmUnfreeze, titleVisibility: .visible) {
            let unfreeze = ichorWindows.map { ArgoFreezeOptions(window: $0.id) }
            if let project {
                Button(String(localized: "End")) { Task { await freeze(.unfreeze, on: project, options: unfreeze) } }
                if app.sync == .outOfSync && app.canSync {
                    Button(String(localized: "End and sync now")) {
                        Task {
                            if await freeze(.unfreeze, on: project, options: unfreeze) { await run(.sync, on: app, options: ArgoSyncOptions(defaultsFor: app)) }
                        }
                    }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(verbatim: unfreezeMessage(app, covers: ichorWindows.map(\.apps).max() ?? 1))
        }
        .sheet(item: $syncSheet) { request in
            ArgoSyncSheet(app: app, resources: request.resources) { options in
                await run(.sync, on: app, options: options)
                selecting = false
                selection = []
            }
        }
        .confirmationDialog(confirmRollback.map { String(localized: "Roll \(app.name) back to \($0.label)?") } ?? "",
                            isPresented: $confirmRollback.isPresent(),
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
        // The network graph is read alongside, so the two refresh together.
        let graph = self.network, appNamespace = self.namespace, appName = self.name
        async let traffic: Void = graph.load(with: client, key: key, namespace: appNamespace, name: appName)
        let cluster = model.activeSummary, store = self.store
        await store.refresh($state, key: key, currentKey: { model.argoKey }) {
            try await store.load(with: client, key: key, cluster: cluster)
        }
        await traffic
    }

    /// What ending the freeze puts back (the hand-made changes), and how many apps resume.
    private func unfreezeMessage(_ app: ArgoApp, covers: Int) -> String {
        var lines: [String] = []
        if app.drifted.isEmpty {
            lines.append(String(localized: "Argo CD resumes auto-sync and self-heal for the apps of this freeze."))
        } else {
            lines.append(String(localized: "Argo CD will put these back as Git has them:"))
            lines += app.drifted.prefix(6).map { "\($0.kind) \($0.namespace.isEmpty ? "" : $0.namespace + "/")\($0.name)" }
            lines.append(String(localized: "Is the fix committed?"))
        }
        if covers > 1 { lines.append(String(localized: "This freeze covers \(covers) apps: they all resume.")) }
        return lines.joined(separator: "\n")
    }

    /// Changes the project's sync windows; true when it went through.
    @discardableResult
    private func freeze(_ action: ArgoFreezeAction, on project: ArgoProject, options: [ArgoFreezeOptions]) async -> Bool {
        guard let client = model.client else { return false }
        if let failure = await store.freeze(action, on: project, options: options, with: client) {
            message = failure
            return false
        }
        succeeded += 1
        announce(String(localized: "Done"))
        await load()
        return true
    }

    private func run(_ action: ArgoAction, on app: ArgoApp, options: ArgoSyncOptions? = nil) async {
        guard let client = model.client else { return }
        // Another request (a refresh, the auto-sync switch) is in flight: wait for it rather
        // than drop this one, which the sync sheet has already dismissed for.
        while busy { try? await Task.sleep(for: .milliseconds(100)) }
        busy = true
        defer { busy = false }
        let failure = await store.run(action, on: app, options: options, with: client)
        if recordActionOutcome(failure, message: &message, succeeded: &succeeded) { announce(String(localized: "Done")) }
        await load()
    }
}
