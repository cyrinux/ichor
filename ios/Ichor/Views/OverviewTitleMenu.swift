import SwiftUI
import IchorCore

/// The overview title's menu (shown by the title once it is inline): switch or manage the
/// clusters, upgrades, add a node, and share the incident summary or a link.
struct OverviewTitleMenu: View {
    @Binding var path: [Route]
    let shareIncident: () -> Void
    @Environment(AppModel.self) private var model

    var body: some View {
        // A submenu: with many clusters, the actions below stay in reach without a scroll.
        if (model.summary?.contexts.count ?? 0) > 1 {
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
        }
        Button { path.append(.clusters) } label: { Label("Manage clusters…", systemImage: "square.stack.3d.up") }
        if model.allows(.upgrade) {
            Button { path.append(.clusterUpgrade(version: "")) } label: {
                Label("Upgrade cluster…", systemImage: "arrow.up.circle")
            }
            Button { path.append(.k8sUpgrade) } label: {
                Label("Upgrade Kubernetes…", systemImage: "shippingbox")
            }
        }
        // On Omni, Add a node opens the boot media: Omni joins the machine itself.
        if let context = model.activeSummary, !context.isKube {
            Button { path.append(.joinNode) } label: {
                Label("Add a node…", systemImage: "plus.circle")
            }
        }
        if model.activeSummary != nil {
            Button(action: shareIncident) {
                Label("Share incident summary…", systemImage: "square.and.arrow.up")
            }
            Button { path.append(.runbooks) } label: {
                Label("Runbooks…", systemImage: "list.bullet.rectangle")
            }
        }
        ShareLinkButton(target: .screen(.cluster))
    }
}
