import SwiftUI
import IchorCore

/// An Argo CD app opened from a list: pushed by the screen that lists it, in any navigation stack.
struct ArgoAppRoute: Hashable {
    let namespace: String
    let name: String
    let downNodes: Set<String>
}
/// A freeze asked from a group header of the apps list.
struct FreezeTarget: Identifiable {
    let app: ArgoApp
    let scope: FreezeScope

    var id: String { "\(app.id)/\(scope.rawValue)" }
}

/// The Argo CD Applications of the cluster, driven through their custom resources with the admin
/// kubeconfig Talos issues (os:admin): filter chips with counts, search, grouping, swipe to sync
/// or refresh, several at once in selection mode; a second tab for the ApplicationSets and
/// projects. Reads again every 2 s while a sync runs (and just after an action), otherwise on
/// open and on a pull.
struct ArgoCDView: View {
    /// Hostnames Talos reports not ready, for the likely cause of a problem app.
    let downNodes: Set<String>

    // Explicit: the private @State properties make the memberwise init private.
    init(downNodes: Set<String>) {
        self.downNodes = downNodes
    }

    enum Tab: Hashable { case apps, sets }

    @Environment(AppModel.self) private var model
    @State private var covered = false
    @State private var state: LoadState<ArgoStatus> = .loading
    @State private var tab = Tab.apps
    @State private var filter = ArgoFilter.all
    @State private var grouping = ArgoGrouping.none
    @State private var query = ""
    @State private var editMode = EditMode.inactive
    @State private var selection: Set<String> = []
    /// Apps a batch sync waits on the user's word for.
    @State private var confirmSync: [ArgoApp]?
    @State private var syncTarget: ArgoApp?
    /// A group header's freeze: the app it is anchored on and the scope.
    @State private var freezeTarget: FreezeTarget?
    @State private var busy: Set<String> = []
    @State private var message: String?
    @State private var succeeded = 0

    private var store: ArgoCDStore { .shared }

    var body: some View {
        LoadStateView(state: state, retry: load) { status in
            if !status.installed {
                ContentUnavailableView("Argo CD is not installed", systemImage: "arrow.triangle.branch",
                                       description: Text("No Argo CD Applications were found in this cluster."))
                    .themedBackground()
            } else {
                Group {
                    switch tab {
                    case .apps:
                        ArgoAppsList(status: status, filter: $filter, grouping: grouping, query: query, downNodes: downNodes,
                                     selection: $selection, busy: busy, refresh: load,
                                     onSync: { syncTarget = $0 }, onRefresh: { app in Task { await run(.refresh, on: [app]) } },
                                     onSyncAll: { confirmSync = $0 },
                                     onFreeze: { app, scope in freezeTarget = FreezeTarget(app: app, scope: scope) })
                            .environment(\.editMode, $editMode)
                    case .sets:
                        ArgoAppSetsList(status: status, query: query, downNodes: downNodes, refresh: load)
                    }
                }
                .safeAreaInset(edge: .top) { topBar(status) }
                .toolbar { toolbar(status) }
            }
        }
        .searchable(text: $query, prompt: Text("Name, project, namespace or repo"))
        .task(id: model.argoKey) {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                if Task.isCancelled { return }
                // An app pushed on top polls for itself.
                if !covered && store.shouldPoll(for: model.argoKey) { await load() }
            }
        }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle(Text(verbatim: "Argo CD"))
        .toolbar { ToolbarItem(placement: .primaryAction) { ShareLinkButton(target: .screen(.argoCD)) } }
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(for: ArgoAppRoute.self) { ArgoAppView(namespace: $0.namespace, name: $0.name, downNodes: $0.downNodes) }
        .navigationDestination(for: ArgoWindowsRoute.self) { _ in ArgoWindowsView() }
        .onAppear { covered = false }
        .onDisappear { covered = true }
        .onChange(of: tab) { editMode = .inactive }
        .onChange(of: editMode) { _, mode in if !mode.isEditing { selection = [] } }
        .sheet(item: $freezeTarget) { target in
            if let status = store.status(for: model.argoKey) {
                ArgoFreezeSheet(app: target.app, status: status, scope: target.scope,
                                freeze: { project, options in await freeze(project, options) }, pauseInstead: nil)
            }
        }
        .sheet(item: $syncTarget) { app in
            ArgoSyncSheet(app: app, resources: []) { options in
                await run(.sync, on: [app], options: options)
            }
        }
        .confirmationDialog(confirmSync.map { String(localized: "Sync \($0.count) apps?") } ?? "",
                            isPresented: $confirmSync.isPresent(),
                            titleVisibility: .visible, presenting: confirmSync) { apps in
            Button("Sync") { Task { await run(.sync, on: apps) } }
            Button("Cancel", role: .cancel) {}
        } message: { apps in
            Text(verbatim: apps.prefix(8).map(\.name).joined(separator: ", ") + (apps.count > 8 ? "…" : "")) +
                Text(verbatim: "\n\n") + Text("Each app syncs to its target revision with its own sync options. Nothing is pruned.")
        }
        .messageAlert($message)
        .sensoryFeedback(.success, trigger: succeeded)
    }

    private func topBar(_ status: ArgoStatus) -> some View {
        VStack(spacing: 8) {
            Picker(selection: $tab) {
                Text("Apps").tag(Tab.apps)
                Text("ApplicationSets & projects").tag(Tab.sets)
            } label: {
                EmptyView()
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            if tab == .apps {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(ArgoFilter.allCases) { chip in
                            let count = status.count(chip)
                            if chip == .all || count > 0 || chip == filter {
                                FilterChip(label: chip.label, count: count, selected: filter == chip, dot: chip.dot) {
                                    withAnimation(.snappy) { filter = chip }
                                }
                            }
                        }
                    }
                    .padding(.horizontal)
                }
            }
        }
        .padding(.bottom, 6)
        .background(.bar)
    }

    @ToolbarContentBuilder
    private func toolbar(_ status: ArgoStatus) -> some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            NavigationLink(value: ArgoWindowsRoute()) {
                Label("Sync windows", systemImage: "snowflake")
            }
        }
        if tab == .apps {
            ToolbarItemGroup(placement: .primaryAction) {
                Menu {
                    Picker("Group by", selection: $grouping) {
                        ForEach(ArgoGrouping.allCases) { Text($0.label).tag($0) }
                    }
                } label: {
                    Label("Group by", systemImage: "rectangle.3.group")
                }
                Button(editMode.isEditing ? String(localized: "Done") : String(localized: "Select")) {
                    withAnimation { editMode = editMode.isEditing ? .inactive : .active }
                }
            }
            if editMode.isEditing {
                ToolbarItemGroup(placement: .bottomBar) {
                    let chosen = status.apps.filter { selection.contains($0.id) }
                    let syncable = chosen.filter(\.canSync)
                    Button { confirmSync = syncable } label: {
                        Label(String(localized: "Sync (\(syncable.count))"), systemImage: "arrow.triangle.2.circlepath")
                            .labelStyle(.titleAndIcon)
                    }
                    .disabled(syncable.isEmpty)
                    Spacer()
                    Button {
                        Task {
                            await run(.refresh, on: chosen)
                            editMode = .inactive
                        }
                    } label: {
                        Label(String(localized: "Refresh (\(chosen.count))"), systemImage: "arrow.clockwise").labelStyle(.titleAndIcon)
                    }
                    .disabled(chosen.isEmpty)
                }
            }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = model.argoKey
        if case .loading = state, let known = store.status(for: key) { state = .loaded(known, at: Date()) }
        let cluster = model.activeSummary
        let loaded: LoadState<ArgoStatus> = await .from { try await store.load(with: client, key: key, cluster: cluster) }
        guard key == model.argoKey else { return }
        state = state.refreshed(with: loaded)
    }

    /// Freezes from a group header, then reads again.
    private func freeze(_ project: ArgoProject, _ options: ArgoFreezeOptions) async {
        guard let client = model.client else { return }
        if let failure = await store.freeze(.freeze, on: project, options: [options], with: client) {
            message = failure
        } else {
            succeeded += 1
            announce(String(localized: "Done"))
        }
        await load()
    }

    /// Runs action on apps (one sheet's options for a single sync), then reads again.
    private func run(_ action: ArgoAction, on apps: [ArgoApp], options: ArgoSyncOptions? = nil) async {
        guard let client = model.client, !apps.isEmpty else { return }
        busy.formUnion(apps.map(\.id))
        defer { busy.subtract(apps.map(\.id)) }
        let failure: String?
        if let options, let app = apps.first, apps.count == 1 {
            failure = await store.run(action, on: app, options: options, with: client)
        } else {
            failure = await store.run(action, on: apps, with: client)
        }
        if let failure { message = failure } else { succeeded += 1; announce(String(localized: "Done")) }
        if action == .sync { editMode = .inactive }
        await load()
    }
}
