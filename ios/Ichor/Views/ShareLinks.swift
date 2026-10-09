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
    /// kubeconfig (`kube`) the Talos screens have nothing to show: its home opens instead.
    func route(client: TalosClient?, kube: Bool = false) async -> Route? {
        if kube, [Target.node, .etcd, .health].contains(target) { return nil }
        if target == .node {
            guard let nodes = try? await client?.overview().nodes,
                  let node = nodes.first(where: { !addr.isEmpty && $0.node == addr || !host.isEmpty && $0.hostname == host })
            else { return nil }
            return .nodeTab(NodeRef(address: node.node, hostname: node.hostname, role: node.role),
                            tab: NodeDetailView.Tab.allCases.first { $0.rawValue.lowercased() == tab } ?? .services)
        }
        return switch target {
        case .cluster: nil
        case .etcd: .etcd
        case .health: .health
        case .argoCD: .argoCD(downNodes: [])
        case .flux: .flux(downNodes: [])
        case .node: nil
        case .argoApp: .argoApp(namespace: namespace, name: name)
        case .fluxApp: .fluxApp(kind: kind, namespace: namespace, name: name)
        case .workloads, .workload, .pod, .cronJob: kubeFocus.map { Route.kubernetes($0) }
        case .data: .dataServices(hints: "", downNodes: [], kind: dataServiceKind)
        case .checkup: .checkup
        case .alerts: .alerts
        }
    }
}

extension PagedList {
    /// The rows loaded so far: none while the first page loads or after a failure.
    var loadedItems: [T] {
        if case .loaded(let load, _, _) = state { load.items } else { [] }
    }
}
