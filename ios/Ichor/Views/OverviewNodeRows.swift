import SwiftUI
import IchorCore

/// The overview's nodes section: site by site in the map's order, chips or full rows, or the
/// dense summary on a large cluster; its header counts them and opens the Nodes screen.
struct OverviewNodesSection: View {
    let overview: ClusterOverview
    @Binding var expanded: Bool
    let tourActive: Bool
    /// The node the demo tour's coach mark points at (its address), if any.
    let tourNode: String?
    let canDetectIPs: Bool
    let detectIPs: () -> Void
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model
    @Environment(\.horizontalSizeClass) private var sizeClass
    /// The overview's measured width: a half-screen Split View does not split the nodes list.
    @Environment(\.availableWidth) private var availableWidth

    var body: some View {
        Section {
            // In the map's order, site by site, once the cluster map is loaded.
            let groups = groupNodes(overview.nodes, by: TopologyStore.shared.topology(for: model.topologyKey))
            let version = overview.nodes.sharedVersion
            if isDenseCluster(overview.nodes.count) {
                // Too many for a chip each: counts, dots, and the problems capped.
                DenseNodes(groups: groups, path: $path)
            } else {
                ForEach(groups) { group in
                    // Site headers only when there is more than one: a single site says nothing.
                    if groups.count > 1 { SiteHeader(title: group.title) }
                    NodeGroupRows(nodes: group.nodes, expanded: expanded, sharedVersion: version,
                                  tourNode: tourNode, path: $path)
                }
            }
        } header: {
            HStack {
                Text("Nodes")
                    .demoTourTip(.nodes, active: tourActive)
                Spacer()
                if model.isDetectingPublicIPs || canDetectIPs {
                    DetectPublicIPsButton(running: model.isDetectingPublicIPs, start: detectIPs)
                }
                Text(verbatim: "\(overview.nodes.count)")
                if isDenseCluster(overview.nodes.count) || ListDetailLayout.isSplit(sizeClass, width: availableWidth ?? .infinity) {
                    // A section holding hundreds of rows defeats the overview: a screen instead. On
                    // an iPad, also the list beside a node's hub.
                    Button { path.append(.nodes(filter: nil, nodes: overview.nodes.overviewOrder)) } label: {
                        Image(systemName: "chevron.right")
                    }
                    .accessibilityLabel(Text("Show all nodes"))
                }
                if !isDenseCluster(overview.nodes.count) {
                    Button { withAnimation { expanded.toggle() } } label: {
                        Image(systemName: expanded ? "chevron.up" : "chevron.down")
                    }
                    .accessibilityLabel(expanded ? Text("Show less") : Text("Show all"))
                }
            }
        }
    }
}

/// One site's nodes in the overview's nodes section. Collapsed (the default): a chip per node,
/// one with a problem tinted in its status colour so it stands out without growing the card;
/// expanded: a full row per node with the details.
private struct NodeGroupRows: View {
    let nodes: [NodeOverview]
    let expanded: Bool
    let sharedVersion: String?
    /// The node the demo tour's coach mark points at (its address), if any.
    var tourNode: String?
    @Binding var path: [Route]

    var body: some View {
        if expanded {
            ForEach(nodes) { node in
                NodeListRow(node: node, sharedVersion: sharedVersion, path: $path)
                    .demoTourTip(.degradedNode, active: node.node == tourNode)
            }
        } else {
            ChipFlow(spacing: 8) {
                ForEach(nodes) { node in
                    NodeChip(node: node) { path.append(.node(node.ref)) }
                        .contextMenu { NodeMenu(node: node, path: $path) }
                        .demoTourTip(.degradedNode, active: node.node == tourNode)
                }
            }
            .buttonStyle(.borderless)
            .padding(.vertical, 4)
        }
    }
}

/// A node in the collapsed nodes section: its status dot and hostname, the whole chip tinted
/// when it has a problem since no row spells it out; tap opens it.
private struct NodeChip: View {
    let node: NodeOverview
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            NodeChipLabel(name: node.hostname, status: node.status)
        }
        .foregroundStyle(node.status == .ready ? Color.primary : node.status.color)
        .accessibilityLabel(Text(verbatim: "\(node.hostname), \(node.status.label)"))
        .accessibilityIdentifier(SmokeID.node)
    }
}

/// A collapsed Nodes section's chip, Talos or Kubernetes: the status dot and `name` on a
/// capsule tinted by a problem. The caller sets the foreground and wraps it (a button, a menu).
struct NodeChipLabel: View {
    let name: String
    let status: NodeStatus

    var body: some View {
        HStack(spacing: 6) {
            Circle().fill(status.color).frame(width: 8, height: 8)
            Text(verbatim: name).font(.subheadline).lineLimit(1)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(status == .ready ? Color.secondary.opacity(0.12) : status.color.opacity(0.16), in: Capsule())
    }
}
