import SwiftUI
import IchorCore

/// Every node of a large cluster, where the dense overview only shows dots: search by hostname
/// or address, filter by health, the same rows and actions as the overview. Opened with the
/// overview's nodes; a pull asks again.
struct NodesView: View {
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @State private var nodes: [NodeOverview]
    @State private var filter: NodeFilter?
    /// A site's NodeGroup key ("" for the nodes the map misses), nil for all.
    @State private var site: String?
    @State private var query = ""

    init(nodes: [NodeOverview], filter: NodeFilter?, path: Binding<[Route]>) {
        _nodes = State(initialValue: nodes)
        _filter = State(initialValue: filter)
        _path = path
    }

    var body: some View {
        // In the map's order, site by site, as on the overview.
        let groups = groupNodes(nodes, by: TopologyStore.shared.topology(for: model.topologyKey))
        let shown = groups.filtered(query: query, filter: filter, site: site)
        let version = nodes.sharedVersion
        List {
            ForEach(shown) { group in
                Section {
                    ForEach(group.nodes) { node in
                        NodeListRow(node: node, sharedVersion: version, path: $path)
                    }
                } header: {
                    if groups.count > 1 { Text(verbatim: group.title) }
                }
            }
        }
        .overlay {
            if shown.isEmpty {
                ContentUnavailableView("No node matches", systemImage: "magnifyingglass")
            }
        }
        .safeAreaInset(edge: .top) { filterBar(groups: groups) }
        .searchable(text: $query, prompt: Text("Hostname or address"))
        .refreshable { await reload() }
        .navigationTitle("Nodes")
        .navigationBarTitleDisplayMode(.inline)
        .themedBackground()
    }

    /// All, needing attention, or one health, each with its count; then, with more than one
    /// site, all sites or one (a site filter only says something with several).
    private func filterBar(groups: [NodeGroup]) -> some View {
        VStack(spacing: 6) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    FilterChip(label: String(localized: "All"), count: nodes.count, selected: filter == nil) {
                        withAnimation(.snappy) { filter = nil }
                    }
                    ForEach(NodeFilter.allCases, id: \.self) { chip in
                        FilterChip(label: chip.label, count: nodes.filter(chip.matches).count, selected: filter == chip, dot: chip.dot) {
                            withAnimation(.snappy) { filter = chip }
                        }
                    }
                }
                .padding(.horizontal)
            }
            if groups.count > 1 {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        FilterChip(label: String(localized: "All sites"), count: nodes.count, selected: site == nil) {
                            withAnimation(.snappy) { site = nil }
                        }
                        ForEach(groups) { group in
                            FilterChip(label: group.title, count: group.nodes.count, selected: site == group.key) {
                                withAnimation(.snappy) { site = group.key }
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

    private func reload() async {
        guard let client = model.client, let overview = try? await client.overview() else { return }
        // Nothing answered: keep what was shown rather than a list of red nodes.
        guard overview.outage == nil else { return }
        nodes = overview.nodes.overviewOrder
    }
}

extension NodeFilter {
    var label: String {
        switch self {
        case .attention: String(localized: "Attention")
        case .ready: NodeHealth.ready.label
        case .notReady: NodeHealth.notReady.label
        case .unreachable: NodeHealth.unreachable.label
        }
    }

    var dot: Color? {
        switch self {
        case .attention: nil
        case .ready: NodeHealth.ready.color
        case .notReady: NodeHealth.notReady.color
        case .unreachable: NodeHealth.unreachable.color
        }
    }
}
