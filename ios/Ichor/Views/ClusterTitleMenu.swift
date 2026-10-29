import SwiftUI
import IchorCore

/// The cluster menu of both homes, grouped by intent (`clusterMenuGroups`): which cluster,
/// operate it, share it. Shown by the title once it is inline, and under ⋯ → Cluster before.
struct ClusterTitleMenu: View {
    @Binding var path: [Route]
    let shareIncident: () -> Void
    @Environment(AppModel.self) private var model

    var body: some View {
        let groups = clusterMenuGroups(context: model.activeSummary, canSwitch: (model.summary?.contexts.count ?? 0) > 1)
        ForEach(Array(groups.enumerated()), id: \.offset) { index, group in
            if index > 0 { Divider() }
            ForEach(group, id: \.self, content: entry)
        }
    }

    @ViewBuilder
    private func entry(_ item: ClusterMenuItem) -> some View {
        switch item {
        case .switchCluster:
            // A submenu: with many clusters, the actions below stay in reach without a scroll.
            Menu {
                ForEach(model.summary?.contexts ?? []) { context in
                    Button { model.activeContext = context.name } label: {
                        if context.name == model.activeContext {
                            Label(model.labels.of(context), systemImage: "checkmark")
                        } else {
                            Text(model.labels.of(context))
                        }
                    }
                }
            } label: {
                Label("Switch cluster", systemImage: "arrow.left.arrow.right")
            }
        case .manage:
            Button { path.append(.clusters) } label: { Label("Manage clusters…", systemImage: "square.stack.3d.up") }
        case .upgradeCluster:
            Button { path.append(.clusterUpgrade(version: "")) } label: {
                Label("Upgrade cluster…", systemImage: "arrow.up.circle")
            }
        case .upgradeKubernetes:
            Button { path.append(.k8sUpgrade) } label: { Label("Upgrade Kubernetes…", systemImage: "shippingbox") }
        case .addNode:
            // On Omni, Add a node opens the boot media: Omni joins the machine itself.
            Button { path.append(.joinNode) } label: { Label("Add a node…", systemImage: "plus.circle") }
        case .updates:
            Button { path.append(.updates) } label: { Label("Updates…", systemImage: "arrow.up.square") }
        case .runbooks:
            Button { path.append(.runbooks) } label: { Label("Runbooks…", systemImage: "list.bullet.rectangle") }
        case .incidentSummary:
            Button(action: shareIncident) { Label("Share incident summary…", systemImage: "square.and.arrow.up") }
        case .shareLink:
            ShareLinkButton(target: .screen(.cluster))
        }
    }
}

/// ⋯ → Cluster: the same menu, in reach before the large title collapses.
struct ClusterSubmenu: View {
    @Binding var path: [Route]
    let shareIncident: () -> Void

    var body: some View {
        Menu {
            ClusterTitleMenu(path: $path, shareIncident: shareIncident)
        } label: {
            Label("Cluster", systemImage: "externaldrive")
        }
    }
}
