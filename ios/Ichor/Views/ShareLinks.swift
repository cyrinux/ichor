import SwiftUI
import IchorCore

extension AppModel {
    /// The link to `target` on the cluster on screen; nil when there is none to name.
    func shareLink(for target: ShareTarget) -> URL? {
        guard let cluster = activeSummary?.clusterID, !cluster.isEmpty else { return nil }
        var target = target
        target.cluster = cluster
        return TalosClient.shareLink(for: target)
    }
}

/// Shares the link to `target` (a toolbar item or a menu entry); nothing when it has none.
struct ShareLinkButton: View {
    let target: ShareTarget

    @Environment(AppModel.self) private var model

    var body: some View {
        if let url = model.shareLink(for: target) {
            ShareLink(item: url) { Label("Share link", systemImage: "link") }
        }
    }
}

extension ShareTarget {
    /// The screen the link opens over the overview; nil for the overview itself. A node only
    /// when it is one of the cluster's (with its role, for the control-plane warnings): a link
    /// cannot point the app's Talos calls at another address. On a cluster added from a
    /// kubeconfig (`kube`) the Talos screens have nothing to show: its home opens instead, but a
    /// node of it (a node or disk alert) opens its nodes searched for that node.
    func route(client: TalosClient?, kube: Bool = false) async -> Route? {
        if kube, target == .node || target == .storage { return await kubeNodeRoute(client: client) }
        if kube, [Target.node, .etcd, .health, .storage, .hardware].contains(target) { return nil }
        if target == .node || target == .storage || target == .hardware {
            guard let nodes = try? await client?.overview().nodes,
                  let node = nodes.first(where: { !addr.isEmpty && $0.node == addr || !host.isEmpty && $0.hostname == host })
            else { return nil }
            if target == .storage { return .storage(node: node.node, hostname: node.hostname) }
            if target == .hardware { return .hardware(node: node.node, hostname: node.hostname) }
            let ref = NodeRef(address: node.node, hostname: node.hostname, role: node.role)
            // A link that names a tab opens the tabs on it; one without (or an unknown one) the hub.
            guard let shown = NodeTabsView.Tab.allCases.first(where: { $0.rawValue.lowercased() == tab }) else { return .node(ref) }
            return .nodeTab(ref, tab: shown)
        }
        return switch target {
        case .cluster: nil
        case .etcd: .etcd
        case .health: .health
        case .argoCD: .argoCD(downNodes: [])
        case .flux: .flux(downNodes: [])
        case .node, .storage, .hardware: nil
        case .argoApp: .argoApp(namespace: namespace, name: name)
        case .fluxApp: .fluxApp(kind: kind, namespace: namespace, name: name)
        case .workloads, .workload, .pod, .cronJob: kubeFocus.map { Route.kubernetes($0) }
        // A plugin finding's alert: the plugin's screen.
        case .data: pluginID.map { Route.plugin(id: $0) } ?? .dataServices(hints: "", downNodes: [], kind: dataServiceKind, solo: dataServiceKind?.opensAlone == true)
        case .checkup: .checkup
        case .alerts: .alerts
        }
    }

    /// The nodes of a kubeconfig cluster searched for the node the link names (its Kubernetes
    /// name); nil (its home) when that node is not one of them.
    private func kubeNodeRoute(client: TalosClient?) async -> Route? {
        let name = addr.isEmpty ? host : addr
        guard !name.isEmpty, let nodes = try? await client?.kubeNodes().nodes,
              nodes.contains(where: { $0.name == name }) else { return nil }
        return .kubeNodes(filter: nil, nodes: nodes, query: name)
    }
}

extension PagedList {
    /// The rows loaded so far: none while the first page loads or after a failure.
    var loadedItems: [T] {
        if case .loaded(let load, _, _) = state { load.items } else { [] }
    }
}
