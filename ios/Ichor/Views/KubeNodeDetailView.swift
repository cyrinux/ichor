import SwiftUI
import IchorCore

/// A Kubernetes node beside the nodes list on an iPad: its object view (summary with
/// conditions, capacity, taints and labels, events, then YAML), with the node menu's actions
/// (cordon, drain, taints and labels, debug shell) in the toolbar. The cordon asks
/// `cordoning` for the list's confirmation; the drain and the editor push over the list.
/// `onDeleted` clears the pick: in a split there is nothing to go back to.
struct KubeNodeDetailView: View {
    let node: KubeNodeInfo
    @Binding var path: [Route]
    @Binding var cordoning: KubeNodeInfo?
    let onDeleted: () -> Void

    var body: some View {
        KubeObjectView(resource: KubeAPIResource(resource: "nodes", kind: "Node", namespaced: false),
                       namespace: "", name: node.name, onDeleted: onDeleted)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        KubeNodeMenu(node: node, path: $path, cordoning: $cordoning)
                    } label: {
                        Label("Node actions", systemImage: "ellipsis.circle")
                    }
                }
            }
    }
}
