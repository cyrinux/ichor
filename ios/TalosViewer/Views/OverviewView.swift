import SwiftUI
import TalosViewerCore

struct OverviewView: View {
    @Environment(AppModel.self) private var model
    @Environment(SupportPrompt.self) private var support
    @State private var state: LoadState<ClusterOverview> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { overview in
            List {
                if support.visible { Section { SupportCard(prompt: support) } }
                Section {
                    Summary(nodes: overview.nodes)
                } header: {
                    if let access = model.activeSummary?.accessLabel { Text(access) }
                }
                Section {
                    ForEach(sorted(overview.nodes)) { node in
                        if node.reachable {
                            NavigationLink(value: Route.node(NodeRef(address: node.node, hostname: node.hostname, role: node.role))) {
                                NodeRow(node: node)
                            }
                        } else {
                            NodeRow(node: node)
                        }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(model.activeContext)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if model.allows(.health) {
                    NavigationLink(value: Route.health) { Image(systemName: "heart.text.square") }
                }
                NavigationLink(value: Route.kubespan) { Image(systemName: "point.3.connected.trianglepath.dotted") }
                NavigationLink(value: Route.etcd) { Image(systemName: "cylinder.split.1x2") }
                NavigationLink(value: Route.settings) { Image(systemName: "gearshape") }
            }
        }
        .task(id: model.activeContext) { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        if case .loaded = state {} else { state = .loading }
        state = await .from { try await client.overview() }
    }

    private func sorted(_ nodes: [NodeOverview]) -> [NodeOverview] {
        // Control-plane nodes first, then by hostname.
        nodes.sorted { (rank($0), $0.hostname) < (rank($1), $1.hostname) }
    }

    private func rank(_ node: NodeOverview) -> Int {
        node.role == "controlplane" ? 0 : 1
    }
}

private struct Summary: View {
    let nodes: [NodeOverview]

    var body: some View {
        HStack(spacing: 24) {
            ForEach(NodeHealth.allCases, id: \.self) { health in
                let count = nodes.filter { $0.health == health }.count
                VStack(alignment: .leading) {
                    Text("\(count)").font(.title.bold()).foregroundStyle(count > 0 ? health.color : .secondary)
                    Text(health.label.lowercased()).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }
}

private struct NodeRow: View {
    let node: NodeOverview

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                VStack(alignment: .leading) {
                    Text(node.hostname).font(.headline)
                    Text(node.node).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                StatusPill(label: node.health.label, color: node.health.color)
            }
            if node.reachable {
                Text([node.role == "controlplane" ? "control plane" : node.role, node.version, node.stage, node.arch]
                    .filter { !$0.isEmpty }.joined(separator: "  ·  "))
                    .font(.caption)
            }
            ForEach(node.unmetConditions, id: \.self) {
                Text("\($0.name): \($0.reason)").font(.caption).foregroundStyle(.orange)
            }
            if let error = node.error, !error.isEmpty {
                Text(error).font(.caption).foregroundStyle(.red)
            }
        }
        .padding(.vertical, 2)
    }
}
