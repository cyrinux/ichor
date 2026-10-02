import SwiftUI
import IchorCore

/// The apps running in the cluster (os:reader): a grid of the recognised ones, filterable by
/// category or by what needs a look (drifting versions, unpinned images), with the Kubernetes
/// and Talos plumbing and the apps the catalog does not know in collapsed groups.
struct AppsView: View {
    /// Address → hostname of the overview's nodes, to name the nodes pods run on.
    let hostnames: [String: String]

    // Explicit: the private @State properties make the memberwise init private.
    init(hostnames: [String: String]) {
        self.hostnames = hostnames
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ClusterInventory> = .loading
    @State private var query = ""
    @State private var filter = AppFilter.all
    @State private var showSystem = false
    @State private var showUnrecognised = false
    @State private var selected: InventoryApp?
    /// The route's hostnames were named before a screenshot mode change: then pods show addresses.
    @State private var hostnamesStale = false

    private let columns = [GridItem(.adaptive(minimum: 96, maximum: 160), spacing: 12)]

    var body: some View {
        LoadStateView(state: state, retry: load) { inventory in
            let active = activeFilter(in: inventory.apps)
            let sections = InventorySections(filterApps(inventory.apps, query: query, filter: active))
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text(inventory.localizedSummary)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                    chips(inventory.apps, active: active)
                    if !sections.main.isEmpty { grid(sections.main) }
                    if !sections.system.isEmpty {
                        AppGroup(title: String(localized: "Kubernetes & Talos · \(sections.system.count)"),
                                 apps: sections.system, showIcons: true, expanded: $showSystem) { grid($0) }
                    }
                    if !sections.unrecognised.isEmpty {
                        AppGroup(title: String(localized: "Not recognised · \(sections.unrecognised.count)"),
                                 apps: sections.unrecognised, showIcons: false, expanded: $showUnrecognised) { grid($0) }
                    }
                }
                .padding()
            }
            .overlay {
                if inventory.apps.isEmpty {
                    ContentUnavailableView("No apps", systemImage: "square.grid.2x2",
                                           description: Text("No Kubernetes containers run in this cluster."))
                } else if sections.main.isEmpty && sections.system.isEmpty && sections.unrecognised.isEmpty {
                    ContentUnavailableView.search(text: query)
                }
            }
            .refreshable { await load() }
            .background(Color(.systemGroupedBackground))
            .themedBackground()
        }
        .searchable(text: $query, prompt: Text("Name, namespace or image"))
        .navigationTitle("Apps")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { Task { await load() } } label: { Image(systemName: "arrow.clockwise") }
                    .accessibilityLabel(Text("Refresh"))
            }
        }
        // A search or a chip may match apps of the collapsed groups: show them.
        .onChange(of: query) { _, text in if !text.isEmpty { expandGroups() } }
        .onChange(of: filter) { _, chip in if chip != .all { expandGroups() } }
        .sheet(item: $selected) { app in AppDetailSheet(app: app, hostnames: hostnamesStale ? [:] : hostnames) }
        // Screenshot mode toggled: nothing loaded before (names, logos) stays on screen.
        .onChange(of: model.dataGeneration) {
            selected = nil
            hostnamesStale = true
            state = .loading
        }
        .task(id: model.dataGeneration) { await load() }
    }

    private func grid(_ apps: [InventoryApp]) -> some View {
        LazyVGrid(columns: columns, spacing: 12) {
            ForEach(apps) { app in
                Button { selected = app } label: { AppTile(app: app) }
                    .buttonStyle(.plain)
            }
        }
    }

    /// All, Attention (when some app needs a look), then each category present.
    private func chips(_ apps: [InventoryApp], active: AppFilter) -> some View {
        let attention = apps.filter(\.needsAttention).count
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                FilterChip(label: String(localized: "All"), count: apps.count, selected: active == .all) { filter = .all }
                if attention > 0 {
                    FilterChip(label: String(localized: "Attention"), count: attention, selected: active == .attention,
                               dot: attentionColor) { filter = .attention }
                }
                ForEach(categoryCounts(apps), id: \.category) { item in
                    FilterChip(label: item.category.label, count: item.count, selected: active == .category(item.category)) {
                        filter = .category(item.category)
                    }
                }
            }
        }
        .scrollClipDisabled()
    }

    /// The chosen chip, or All once a refresh removed it (nothing left to look at, say).
    private func activeFilter(in apps: [InventoryApp]) -> AppFilter {
        switch filter {
        case .all: .all
        case .attention: apps.contains(where: \.needsAttention) ? .attention : .all
        case .category(let category): apps.contains(where: { $0.category == category }) ? filter : .all
        }
    }

    private func expandGroups() {
        showSystem = true
        showUnrecognised = true
    }

    /// A failed refresh keeps the apps shown (like the overview's card); a result that started
    /// before a screenshot mode change is dropped, so no logo or name from before comes back.
    private func load() async {
        guard let client = model.client else { return }
        if case .loaded = state {} else { state = .loading }
        let generation = model.dataGeneration
        let loaded: LoadState<ClusterInventory> = await .from { try await client.inventory() }
        guard generation == model.dataGeneration else { return }
        if case .failed = loaded, case .loaded = state { return }
        state = loaded
    }
}

/// A collapsible group under the grid, on a rounded card; its header may show a few icons.
private struct AppGroup<Content: View>: View {
    let title: String
    let apps: [InventoryApp]
    let showIcons: Bool
    @Binding var expanded: Bool
    @ViewBuilder let content: ([InventoryApp]) -> Content

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            content(apps).padding(.top, 12)
        } label: {
            HStack(spacing: 8) {
                Text(verbatim: title)
                    .font(.headline)
                    .foregroundStyle(.primary)
                Spacer(minLength: 8)
                if showIcons && !expanded {
                    HStack(spacing: -6) {
                        ForEach(apps.prefix(3)) { AppIconView(app: $0, size: 24) }
                    }
                }
            }
        }
        .padding()
        .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}
