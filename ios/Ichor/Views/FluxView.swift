import SwiftUI
import IchorCore

/// A Flux Kustomization or HelmRelease opened from a list: pushed by the screen that lists it,
/// in any navigation stack.
struct FluxAppRoute: Hashable {
    let kind: String
    let namespace: String
    let name: String
    let downNodes: Set<String>
}

/// The Flux Kustomizations and HelmReleases of the cluster, driven through their custom
/// resources with the admin kubeconfig Talos issues (os:admin): filter chips with counts,
/// search, swipe to reconcile, suspend or resume; a second tab for the sources. Reads again
/// every 2 s while something reconciles (and just after an action), otherwise on open and on
/// a pull.
struct FluxView: View {
    /// Hostnames Talos reports not ready, for the pods of a failing app.
    let downNodes: Set<String>

    // Explicit: the private @State properties make the memberwise init private.
    init(downNodes: Set<String>) {
        self.downNodes = downNodes
    }

    enum Tab: Hashable { case apps, sources }

    /// An action waiting for the user's word.
    struct Pending: Identifiable {
        let action: FluxAction
        let target: FluxRef
        /// For a suspend: the Kustomization that may put it back.
        var notice: String?
        var id: String { "\(action.rawValue)/\(target.kind)/\(target.namespace)/\(target.name)" }
    }

    @Environment(AppModel.self) private var model
    @State private var covered = false
    @State private var state: LoadState<FluxStatus> = .loading
    @State private var tab = Tab.apps
    @State private var filter = FluxFilter.all
    @State private var query = ""
    @State private var confirm: Pending?
    @State private var busy: Set<FluxRef> = []
    @State private var message: String?
    @State private var succeeded = 0

    private var store: FluxStore { .shared }

    var body: some View {
        LoadStateView(state: state, retry: load) { status in
            if !status.installed {
                ContentUnavailableView("Flux is not installed", systemImage: "arrow.triangle.branch",
                                       description: Text("No Flux Kustomizations or HelmReleases were found in this cluster."))
                    .themedBackground()
            } else {
                Group {
                    switch tab {
                    case .apps:
                        FluxAppsList(status: status, filter: $filter, query: query, downNodes: downNodes, busy: busy, refresh: load,
                                     act: act)
                    case .sources:
                        FluxSourcesList(status: status, query: query, busy: busy, refresh: load, act: act)
                    }
                }
                .safeAreaInset(edge: .top) { topBar(status) }
            }
        }
        .searchable(text: $query, prompt: Text("Name, namespace, path or URL"))
        .task(id: model.fluxKey) {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                if Task.isCancelled { return }
                // An app pushed on top polls for itself.
                if !covered && store.shouldPoll(for: model.fluxKey) { await load() }
            }
        }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle(Text(verbatim: "Flux"))
        .toolbar { ToolbarItem(placement: .primaryAction) { ShareLinkButton(target: .screen(.flux)) } }
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(for: FluxAppRoute.self) {
            FluxAppView(kind: $0.kind, namespace: $0.namespace, name: $0.name, downNodes: $0.downNodes)
        }
        .onAppear { covered = false }
        .onDisappear { covered = true }
        .confirmationDialog(confirm.map { $0.action.title(for: $0.target.name) } ?? "",
                            isPresented: $confirm.isPresent(),
                            titleVisibility: .visible, presenting: confirm) { pending in
            Button(pending.action.label, role: pending.action == .suspend ? ButtonRole.destructive : nil) {
                Task { await run(pending.action, on: pending.target) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { pending in
            Text(verbatim: [pending.action.explanation(for: pending.target.name), pending.action == .suspend ? pending.notice : nil]
                .compactMap { $0 }.joined(separator: "\n\n"))
        }
        .messageAlert($message)
        .sensoryFeedback(.success, trigger: succeeded)
    }

    private func topBar(_ status: FluxStatus) -> some View {
        VStack(spacing: 8) {
            Picker(selection: $tab) {
                Text("Apps").tag(Tab.apps)
                Text("Sources").tag(Tab.sources)
            } label: {
                EmptyView()
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            if tab == .apps {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(FluxFilter.allCases) { chip in
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

    /// Reconcile runs at once (Flux would on its interval anyway); the rest asks first.
    private func act(_ action: FluxAction, _ target: FluxRef) {
        if action == .reconcile {
            Task { await run(action, on: target) }
        } else {
            var notice: String?
            if case .loaded(let status, _, _) = state {
                notice = status.app(kind: target.kind, namespace: target.namespace, name: target.name)?.ownerNotice
            }
            confirm = Pending(action: action, target: target, notice: notice)
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = model.fluxKey
        if case .loading = state, let known = store.status(for: key) { state = .loaded(known, at: Date()) }
        let loaded: LoadState<FluxStatus> = await .from { try await store.load(with: client, key: key) }
        guard key == model.fluxKey else { return }
        state = state.refreshed(with: loaded)
    }

    /// Runs action on target, then reads again.
    private func run(_ action: FluxAction, on target: FluxRef) async {
        guard let client = model.client, !busy.contains(target) else { return }
        busy.insert(target)
        defer { busy.remove(target) }
        if let failure = await store.run(action, on: target, with: client) {
            message = failure
        } else {
            succeeded += 1
            announce(String(localized: "Done"))
        }
        await load()
    }
}
