import SwiftUI
import IchorCore

/// Every node of a large cluster added from a kubeconfig, where the dense home section only
/// shows dots: search by name or address, filter by status, the nodes grouped by status (the
/// worst first), the same rows and actions as home. Opened with the home's nodes; a pull asks
/// again. On an iPad split the picked node shows beside the list (`KubeNodeDetailView`, its
/// menu in the toolbar); alone (iPhone, a narrow window) a row stays the menu.
struct KubeNodesView: View {
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @State private var nodes: [KubeNodeInfo]
    @State private var filter: NodeFilter?
    @State private var query = ""
    /// The node whose cordon to change, after a confirmation.
    @State private var cordoning: KubeNodeInfo?
    /// The name of the node shown beside the list: a name, so a reload shows its new state.
    @State private var selected: String?
    /// Whether the list has the detail beside it (AdaptiveListDetail).
    @State private var split = false

    init(nodes: [KubeNodeInfo], filter: NodeFilter?, query: String = "", path: Binding<[Route]>) {
        _nodes = State(initialValue: nodes)
        _filter = State(initialValue: filter)
        _query = State(initialValue: query)
        _path = path
    }

    var body: some View {
        AdaptiveListDetail(selection: $selected, split: $split, placeholder: "Pick a node to see its details",
                           systemImage: "server.rack") {
            nodeList
        } detail: { name in
            if let node = nodes.first(where: { $0.name == name }) {
                KubeNodeDetailView(node: node, path: $path, cordoning: $cordoning, onDeleted: { selected = nil })
            } else {
                ContentUnavailableView("Pick a node to see its details", systemImage: "server.rack").themedBackground()
            }
        }
        .kubeCordonDialogs(cordoning: $cordoning, reload: reload)
        .navigationTitle("Nodes")
        .navigationBarTitleDisplayMode(.inline)
        // Cordon and drain are cluster-wide: loaded here, so the detail's menu is gated too.
        .loadsKubeActionAccess(namespace: "")
    }

    @ViewBuilder
    private var nodeList: some View {
        let groups = nodes.filtered(query: query, filter: filter).byStatus
        List {
            ForEach(groups) { group in
                Section {
                    ForEach(group.nodes) { node in
                        if split {
                            // Beside the detail a tap picks the node; its menu is the detail's toolbar.
                            ListDetailLink(value: node.name) { KubeNodeRow(node: node) }
                        } else {
                            KubeNodeActionRow(node: node, path: $path, cordoning: $cordoning)
                        }
                    }
                } header: {
                    HStack {
                        Text(verbatim: group.status.label)
                        Spacer()
                        Text(verbatim: "\(group.nodes.count)")
                    }
                }
            }
        }
        .overlay {
            if groups.isEmpty {
                ContentUnavailableView("No node matches", systemImage: "magnifyingglass")
            }
        }
        .safeAreaInset(edge: .top) { filterBar }
        .searchable(text: $query, prompt: Text("Hostname or address"))
        .refreshable { await reload() }
        .themedBackground()
    }

    /// All, needing attention, or one status, each with its count: Kubernetes never says unreachable.
    private var filterBar: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                FilterChip(label: String(localized: "All"), count: nodes.count, selected: filter == nil) {
                    withAnimation(.snappy) { filter = nil }
                }
                ForEach(kubeNodeFilters, id: \.self) { chip in
                    FilterChip(label: chip.label, count: nodes.filter(chip.matches).count, selected: filter == chip, dot: chip.dot) {
                        withAnimation(.snappy) { filter = chip }
                    }
                }
            }
            .padding(.horizontal)
        }
        .padding(.bottom, 6)
        .background(.bar)
    }

    private func reload() async {
        guard let client = model.client, let overview = try? await client.kubeNodes() else { return }
        // The credentials may not list nodes: keep what was shown rather than nothing.
        guard !overview.forbidden else { return }
        nodes = overview.nodes
    }
}
