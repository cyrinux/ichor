import SwiftUI
import IchorCore

/// The Kubernetes home's nodes on a cluster too large for a row per node (see isDenseCluster),
/// as the overview's DenseNodes: the status counts, a dot per node grouped by status (the worst
/// first), then the problem nodes as full rows, capped, with "N more" opening the Kubernetes
/// nodes screen on them. A tap on a dot offers the node's actions: there is no node screen
/// without Talos.
struct DenseKubeNodes: View {
    let nodes: [KubeNodeInfo]
    @Binding var path: [Route]
    @Binding var cordoning: KubeNodeInfo?

    var body: some View {
        let problems = nodes.problemNodes()
        HealthSummary(counts: nodes.healthCounts)
        KubeNodeDots(nodes: nodes.byStatus.flatMap(\.nodes), path: $path, cordoning: $cordoning)
        ForEach(problems.shown) { node in
            KubeNodeActionRow(node: node, path: $path, cordoning: $cordoning)
        }
        if problems.more > 0 {
            Button { path.append(.kubeNodes(filter: .attention, nodes: nodes)) } label: {
                HStack {
                    Text("\(problems.more) more need a look")
                    Spacer()
                    Image(systemName: "chevron.right").font(.footnote.weight(.semibold))
                }
            }
            .foregroundStyle(.tint)
        }
    }
}

/// A dot per node, coloured by its status, in the given order (grouped by status: the problem
/// dots lead); a tap offers the node's actions.
private struct KubeNodeDots: View {
    let nodes: [KubeNodeInfo]
    @Binding var path: [Route]
    @Binding var cordoning: KubeNodeInfo?

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: dotTarget, maximum: dotTarget), spacing: 0)], alignment: .leading, spacing: 0) {
            ForEach(nodes) { node in
                Menu {
                    KubeNodeMenu(node: node, path: $path, cordoning: $cordoning)
                } label: {
                    Circle()
                        .fill(node.status.color)
                        .frame(width: 12, height: 12)
                        .frame(width: dotTarget, height: dotTarget)
                        .contentShape(Rectangle())
                }
                .accessibilityLabel(Text(verbatim: "\(node.name), \(node.status.label)"))
            }
        }
        // Each dot its own menu, not the whole row.
        .buttonStyle(.borderless)
        .padding(.vertical, 2)
    }
}

/// Small enough for a few hundred, big enough to hit (the overview's dots).
private let dotTarget: CGFloat = 28
