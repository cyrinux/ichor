import SwiftUI
import IchorCore

/// Every cluster's nodes, pods, workloads, CronJobs, Argo CD and Flux apps, as last seen: the
/// query ranked by the Go core over what the phone keeps, grouped by cluster (the active one
/// first). A result switches to its cluster and opens its screen through `open`, the share-link
/// routing; a screen of the active cluster (the Screens section) is pushed through
/// `openRoute`. Typing never calls a cluster; "Refresh active cluster" does.
struct SearchView: View {
    /// Opens a result's screen on its cluster (MainNavigation's share-target opening).
    let open: (ShareTarget) -> Void
    /// Pushes a screen of the active cluster.
    let openRoute: (Route) -> Void

    @Environment(AppModel.self) private var model
    @State private var store = SearchStore()
    @State private var text = ""
    @State private var presented = false
    @State private var allScreens = false

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
                screensSection
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
        .onChange(of: text) { _, value in
            allScreens = false
            store.search(value)
        }
        .onChange(of: store.nodes) { store.setCatalog(catalog.map(\.entry)) }
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
            store.setCatalog(catalog.map(\.entry))
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

    /// The active cluster's screens, its nodes' included.
    private var catalog: [ScreenSpec] { screenCatalog(model: model, nodes: store.nodes) }

    /// The matching screens above the clusters' objects: 5, then "Show all"; absent with none.
    @ViewBuilder
    private var screensSection: some View {
        let specs = Dictionary(catalog.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let rows = store.screens.results.filter { specs[$0.id] != nil }
        if !rows.isEmpty {
            Section("Screens") {
                ForEach(allScreens ? rows : Array(rows.prefix(5))) { match in
                    if let spec = specs[match.id] {
                        Button {
                            store.remember(text)
                            openRoute(spec.route)
                        } label: {
                            Label {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: match.title)
                                    Text(verbatim: match.subtitle.isEmpty ? match.section : match.subtitle)
                                        .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                                }
                            } icon: {
                                Image(systemName: spec.systemImage)
                            }
                        }
                        .foregroundStyle(.primary)
                    }
                }
                if !allScreens, rows.count > 5 {
                    Button("Show all") { allScreens = true }
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
        } else if typing, store.results.results.isEmpty, store.screens.results.isEmpty {
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
