import SwiftUI
import IchorCore

/// One Flux Kustomization or HelmRelease: the hero with its state and actions, a
/// Kustomization's diff, where it comes from, its conditions, the pods that are not ready, what
/// a Kustomization applies (grouped by kind) and a HelmRelease's history. Every action but Reconcile asks first, saying what it
/// does. Reads again every 2 s while it reconciles (and just after an action).
struct FluxAppView: View {
    let kind: String
    let namespace: String
    let name: String
    let downNodes: Set<String>

    // Explicit: the private @State properties make the memberwise init private.
    init(kind: String, namespace: String, name: String, downNodes: Set<String>) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.downNodes = downNodes
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<FluxStatus> = .loading
    @State private var confirm: FluxAction?
    @State private var busy = false
    @State private var message: String?
    @State private var succeeded = 0

    private var store: FluxStore { .shared }

    var body: some View {
        LoadStateView(state: state, retry: load) { status in
            if let app = status.app(kind: kind, namespace: namespace, name: name) {
                content(app, status: status)
            } else {
                ContentUnavailableView("App not found", systemImage: "questionmark.app",
                                       description: Text("\(name) is no longer a Flux \(kind) of this cluster."))
                    .themedBackground()
            }
        }
        .task(id: model.fluxKey) {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                if Task.isCancelled { return }
                if store.shouldPoll(for: model.fluxKey) { await load() }
            }
        }
        .navigationTitle(Text(verbatim: name))
        .toolbar { ToolbarItem(placement: .primaryAction) { ShareLinkButton(target: .fluxApp(kind: kind, namespace: namespace, name: name)) } }
        .navigationBarTitleDisplayMode(.inline)
        .messageAlert($message)
        .sensoryFeedback(.success, trigger: succeeded)
    }

    private func content(_ app: FluxApp, status: FluxStatus) -> some View {
        List {
            FluxHero(app: app, source: status.source(of: app), busy: busy) { act($0, on: app) }
            if app.isKustomization {
                Section {
                    NavigationLink { FluxDiffView(target: app.target) } label: {
                        Label("Show diff", systemImage: "plus.forwardslash.minus")
                    }
                } footer: {
                    Text("What a reconcile would change now, compared in the cluster without changing anything.")
                }
            }
            FluxConditionsSection(app: app)
            if !app.unhealthyPods.isEmpty { ArgoPodsSection(pods: app.unhealthyPods, downNodes: downNodes) }
            let children = status.children(of: app)
            if !children.isEmpty { FluxChildrenSection(apps: children, downNodes: downNodes) }
            if !app.resources.isEmpty { FluxInventorySection(app: app) }
            if !app.history.isEmpty { FluxHistorySection(history: app.history) }
        }
        .refreshable { await load() }
        .themedBackground()
        .confirmationDialog(confirm.map { $0.title(for: app.name) } ?? "",
                            isPresented: $confirm.isPresent(),
                            titleVisibility: .visible, presenting: confirm) { action in
            Button(action.label, role: action == .suspend ? ButtonRole.destructive : nil) {
                Task { await run(action, on: app) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { action in
            Text(verbatim: [action.explanation(for: app.name), action == .suspend ? app.ownerNotice : nil]
                .compactMap { $0 }.joined(separator: "\n\n"))
        }
    }

    /// Reconcile runs at once (Flux would on its interval anyway); the rest asks first.
    private func act(_ action: FluxAction, on app: FluxApp) {
        if action == .reconcile {
            Task { await run(action, on: app) }
        } else {
            confirm = action
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = model.fluxKey
        if case .loading = state, let known = store.status(for: key) { state = .loaded(known, at: Date()) }
        let loaded: LoadState<FluxStatus> = await .from { try await store.load(with: client, key: key) }
        if key == model.fluxKey { state = state.refreshed(with: loaded) }
    }

    private func run(_ action: FluxAction, on app: FluxApp) async {
        guard let client = model.client else { return }
        // Another request is in flight: wait for it rather than drop this one, which the
        // confirmation has already been dismissed for.
        while busy { try? await Task.sleep(for: .milliseconds(100)) }
        busy = true
        defer { busy = false }
        let failure = await store.run(action, on: app.target, with: client)
        if recordActionOutcome(failure, message: &message, succeeded: &succeeded) { announce(String(localized: "Done")) }
        await load()
    }
}
