import SwiftUI
import IchorCore

/// Every cluster's nodes, pods, workloads, CronJobs, Argo CD and Flux apps, as last seen: the
/// query ranked by the Go core over what the phone keeps, grouped by cluster (the active one
/// first). A result switches to its cluster and opens its screen through `open`, the share-link
/// routing. Typing never calls a cluster; "Refresh active cluster" does.
struct SearchView: View {
    /// Opens a result's screen on its cluster (MainNavigation's share-target opening).
    let open: (ShareTarget) -> Void

    @Environment(AppModel.self) private var model
    @State private var store = SearchStore()
    @State private var text = ""
    @State private var presented = false

    var body: some View {
        List {
            if !model.keepLastKnown {
                Section {
                    Label("Turn on “Keep last known state” in Settings to search your other clusters too", systemImage: "info.circle")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            if let error = store.refreshError {
                Section {
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Could not refresh the active cluster: showing what it last showed")
                            Text(verbatim: error).font(.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "exclamationmark.triangle").foregroundStyle(.orange)
                    }
                }
            }
            if text.trimmingCharacters(in: .whitespaces).isEmpty {
                recentSection
            } else {
                resultSections
            }
        }
        .overlay { emptyState }
        .navigationTitle("Search clusters")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $text, isPresented: $presented, placement: .navigationBarDrawer(displayMode: .always),
                    prompt: Text("Pods, workloads, nodes, apps…"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .onChange(of: text) { _, value in store.search(value) }
        .onSubmit(of: .search) { store.remember(text) }
        .toolbar {
            if canRefresh {
                ToolbarItem(placement: .primaryAction) {
                    if store.refreshing {
                        ProgressView()
                    } else {
                        Button {
                            Task { await store.refresh(model: model) }
                        } label: {
                            Label("Refresh active cluster", systemImage: "arrow.clockwise")
                        }
                    }
                }
            }
        }
        .task {
            await store.build(model: model)
            presented = true
        }
    }

    /// Only a real cluster the role lets list pods and workloads (or read the overview) is refreshed.
    private var canRefresh: Bool {
        guard let context = model.activeSummary, !context.demo, model.client != nil else { return false }
        return !model.activeIsKube || model.allows(.workloads)
    }

    @ViewBuilder
    private var recentSection: some View {
        if !store.recent.isEmpty {
            Section("Recent searches") {
                ForEach(store.recent, id: \.self) { query in
                    Button {
                        text = query
                    } label: {
                        Label(query, systemImage: "clock.arrow.circlepath")
                    }
                    .foregroundStyle(.primary)
                    .swipeActions {
                        Button("Remove", role: .destructive) { store.forget(query) }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var resultSections: some View {
        let activeCluster = model.activeSummary.map { searchCluster(of: $0) } ?? ""
        ForEach(searchSections(store.results.results, activeCluster: activeCluster), id: \.cluster) { section in
            Section {
                ForEach(section.results) { result in
                    SearchResultRow(result: result) {
                        guard let target = result.target else { return }
                        store.remember(text)
                        open(target)
                    }
                }
            } header: {
                Text(verbatim: section.context)
            }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let typing = !text.trimmingCharacters(in: .whitespaces).isEmpty
        if store.building {
            ProgressView()
        } else if store.nothingKnown {
            ContentUnavailableView("Nothing known yet: open a cluster once", systemImage: "magnifyingglass")
        } else if typing, store.results.results.isEmpty {
            ContentUnavailableView.search(text: text)
        }
    }
}

/// A result: its kind's symbol, name, what it is, its namespace and, when old, when it was seen.
private struct SearchResultRow: View {
    let result: SearchResult
    let onOpen: () -> Void

    var body: some View {
        Button(action: onOpen) {
            HStack(spacing: 12) {
                Image(systemName: symbol)
                    .foregroundStyle(.tint)
                    .frame(width: 24)
                    .accessibilityLabel(kindName)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: result.name).font(.body)
                    Text(verbatim: subtitle).font(.caption).foregroundStyle(.secondary)
                    if searchIsStale(result, now: Date()) {
                        Text("Last seen \(result.date.formatted(.relative(presentation: .named)))")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .foregroundStyle(.primary)
        // Without a share target (a name a link cannot carry) there is no screen to open.
        .disabled(result.target == nil)
    }

    private var subtitle: String {
        result.namespace.isEmpty ? result.detail : "\(result.detail) · \(result.namespace)"
    }

    private var symbol: String {
        switch result.kind {
        case "node": "server.rack"
        case "pod": "cube"
        case "workload": "square.stack.3d.up"
        case "cronjob": "clock.arrow.circlepath"
        case "argo-app": "arrow.triangle.2.circlepath"
        case "flux-app": "arrow.triangle.branch"
        default: "questionmark.square"
        }
    }

    private var kindName: Text {
        switch result.kind {
        case "node": Text("Node")
        case "pod": Text("Pod")
        case "workload": Text("Workload")
        case "cronjob": Text("CronJob")
        case "argo-app": Text("Argo CD app")
        case "flux-app": Text("Flux app")
        default: Text(verbatim: result.kind)
        }
    }
}
