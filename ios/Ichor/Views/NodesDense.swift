import SwiftUI
import IchorCore

/// The overview's nodes on a cluster too large for a chip per node (see isDenseCluster): the
/// health counts, a dot per node, then the problem nodes as full rows, capped, with "N more"
/// opening the Nodes screen on them.
struct DenseNodes: View {
    /// In the overview's order.
    let nodes: [NodeOverview]
    @Binding var path: [Route]

    var body: some View {
        let problems = nodes.problemNodes()
        let version = nodes.sharedVersion
        HealthSummary(counts: nodes.healthCounts)
        NodeDots(nodes: nodes, path: $path)
        ForEach(problems.shown) { node in
            NodeListRow(node: node, sharedVersion: version, path: $path)
        }
        if problems.more > 0 {
            Button { path.append(.nodes(filter: .attention, nodes: nodes)) } label: {
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

/// "187 ready · 3 not ready · 1 unreachable", each count in its status colour; zeros left out.
struct HealthSummary: View {
    let counts: HealthCounts

    var body: some View {
        let parts = NodeHealth.allCases.filter { counts[$0] > 0 }
        parts.indices.reduce(Text(verbatim: "")) { text, i in
            text + Text(verbatim: i > 0 ? "  ·  " : "") + Self.count(counts[parts[i]], of: parts[i]).foregroundStyle(parts[i].color)
        }
        .font(.subheadline)
    }

    private static func count(_ n: Int, of health: NodeHealth) -> Text {
        switch health {
        case .ready: Text("\(n) ready")
        case .notReady: Text("\(n) not ready")
        case .unreachable: Text("\(n) unreachable")
        }
    }
}

/// A dot per node, coloured by its health: tap opens it, a long press offers its actions.
private struct NodeDots: View {
    let nodes: [NodeOverview]
    @Binding var path: [Route]

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: dotTarget, maximum: dotTarget), spacing: 0)], alignment: .leading, spacing: 0) {
            ForEach(nodes) { node in
                NodeDot(node: node, path: $path)
            }
        }
        // Each dot its own button, not the whole row.
        .buttonStyle(.borderless)
        .padding(.vertical, 2)
    }
}

/// Small enough for a few hundred, big enough to hit.
private let dotTarget: CGFloat = 28

private struct NodeDot: View {
    let node: NodeOverview
    @Binding var path: [Route]

    var body: some View {
        Group {
            if node.reachable {
                Button { path.append(.node(node.ref)) } label: { dot }
                    .contextMenu { NodeMenu(node: node, path: $path) }
            } else {
                // Nothing to open on a node that does not answer: a tap offers what it still can.
                Menu { NodeMenu(node: node, path: $path) } label: { dot }
            }
        }
        .accessibilityLabel(Text(verbatim: "\(node.hostname), \(node.health.label)"))
    }

    private var dot: some View {
        Circle()
            .fill(node.health.color)
            .frame(width: 12, height: 12)
            .frame(width: dotTarget, height: dotTarget)
            .contentShape(Rectangle())
    }
}
