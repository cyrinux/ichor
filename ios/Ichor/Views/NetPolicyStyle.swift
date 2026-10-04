import SwiftUI
import IchorCore

// How network policies and Cilium flows read everywhere in the app: kind badges, isolation
// colors, peers as chips with an icon, drop reasons and verdicts in plain words.

extension NetPolicyKind {
    var color: Color {
        switch self {
        case .networkPolicy: .blue
        case .cilium: .purple
        case .ciliumClusterwide: .indigo
        case .unknown: .secondary
        }
    }
}

extension NetIsolation {
    /// Fully isolated is the goal, partly is worth a look, none is neutral.
    var color: Color? {
        switch self {
        case .full: .green
        case .partial: attentionColor
        case .open: nil
        }
    }
}

extension NetDirection {
    var label: String {
        switch self {
        case .ingress: String(localized: "Ingress")
        case .egress: String(localized: "Egress")
        }
    }

    var symbol: String {
        switch self {
        case .ingress: "arrow.down.right.circle"
        case .egress: "arrow.up.right.circle"
        }
    }
}

extension NetSubject {
    var text: String {
        switch self {
        case .allPods: String(localized: "All pods")
        case .allPodsOfCluster: String(localized: "All pods of the cluster")
        case .selector(let selector): selector
        case .nodes(let selector): selector.isEmpty ? String(localized: "All nodes") : String(localized: "Nodes \(selector)")
        }
    }
}

extension NetPeer {
    var symbol: String {
        switch kind {
        case .pods: "cube"
        case .namespaces: "square.stack.3d.up"
        case .cidr: "network"
        case .entity: "globe"
        case .fqdn: "link"
        case .service: "point.3.connected.trianglepath.dotted"
        case .nodes: "server.rack"
        case .unknown: "questionmark.circle"
        }
    }

    /// "app=db in media", "All pods in any namespace", "10.0.0.0/8 except 10.1.0.0/16", "world".
    func text(clusterWide: Bool) -> String {
        switch kind {
        case .pods:
            let pods = selector.isEmpty ? String(localized: "All pods") : selector
            switch scope(clusterWide: clusterWide) {
            case .own: return pods
            case .any: return String(localized: "\(pods) in any namespace")
            case .named(let namespace): return String(localized: "\(pods) in \(namespace)")
            case .matching(let namespaces): return String(localized: "\(pods) in namespaces \(namespaces)")
            }
        case .namespaces:
            return namespaceSelector.isEmpty ? String(localized: "All namespaces") : String(localized: "Namespaces \(namespaceSelector)")
        case .cidr:
            return except.isEmpty ? value : String(localized: "\(value) except \(except.joined(separator: ", "))")
        case .entity, .fqdn:
            return value
        case .service:
            return String(localized: "Service \(serviceName)")
        case .nodes:
            return selector.isEmpty ? String(localized: "All nodes") : String(localized: "Nodes \(selector)")
        case .unknown:
            return value.isEmpty ? selector : value
        }
    }
}

extension NetPort {
    /// The label, or "Any port" when it names none.
    var text: String { label.isEmpty ? String(localized: "Any port") : label }
}

extension HubbleVerdict {
    var label: String {
        switch self {
        case .forwarded: String(localized: "Forwarded")
        case .dropped: String(localized: "Dropped")
        case .audit: String(localized: "Would be dropped")
        case .error: String(localized: "Error")
        case .other: String(localized: "Other")
        }
    }

    var color: Color {
        switch self {
        case .forwarded: .green
        case .dropped, .error: .red
        case .audit: attentionColor
        case .other: .secondary
        }
    }
}

extension DropReason {
    var text: String {
        switch self {
        case .policyDenied: String(localized: "No policy allows this traffic")
        case .policyDeny: String(localized: "An explicit deny rule matched")
        case .authRequired: String(localized: "Mutual authentication is required and did not happen")
        case .staleOrUnroutableIP: String(localized: "Stale or unroutable IP address")
        case .ctMapInsertionFailed: String(localized: "The connection tracking table is full")
        case .other(let reason): reason
        case .unspecified: String(localized: "No reason given")
        }
    }
}

extension HubbleAgentPhase {
    var label: String {
        switch self {
        case .connecting: String(localized: "Connecting")
        case .live: String(localized: "Live")
        case .error: String(localized: "Error")
        }
    }

    var color: Color {
        switch self {
        case .connecting: .secondary
        case .live: .green
        case .error: .red
        }
    }
}

extension DropHint {
    var text: String {
        switch self {
        case .allowIngress(let from, let port, _):
            port.isEmpty ? String(localized: "Add an ingress rule allowing \(from) to one of these policies")
                : String(localized: "Add an ingress rule allowing \(from) on \(port) to one of these policies")
        case .allowEgress(let to, let port, _):
            port.isEmpty ? String(localized: "Add an egress rule allowing \(to) to one of these policies")
                : String(localized: "Add an egress rule allowing \(to) on \(port) to one of these policies")
        case .removeDeny:
            String(localized: "A deny rule wins over any allow rule: narrow or remove it in these policies")
        }
    }
}

/// A peer of a rule as a chip: its icon, then what it selects.
struct NetPeerChip: View {
    let peer: NetPeer
    let clusterWide: Bool

    var body: some View {
        Label {
            Text(verbatim: peer.text(clusterWide: clusterWide))
                .font(.caption.monospaced())
                .lineLimit(1)
                .truncationMode(.middle)
        } icon: {
            Image(systemName: peer.symbol).font(.caption)
        }
        .labelStyle(.titleAndIcon)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color(.tertiarySystemFill), in: Capsule())
    }
}

/// "NP", "CNP", "CCNP" in the kind's color.
struct NetPolicyKindBadge: View {
    let kind: NetPolicyKind

    var body: some View {
        Text(verbatim: kind.short)
            .font(.caption2.weight(.bold).monospaced())
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(kind.color)
            .background(kind.color.opacity(0.15), in: RoundedRectangle(cornerRadius: 5, style: .continuous))
            .accessibilityLabel(Text(verbatim: kind.rawValue))
    }
}

/// Lays out chips left to right, wrapping onto new lines.
struct ChipFlow: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let rows = arrange(width: proposal.width ?? .infinity, subviews: subviews)
        let width = rows.map(\.width).max() ?? 0
        let height = rows.map(\.height).reduce(0, +) + spacing * CGFloat(max(rows.count - 1, 0))
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for row in arrange(width: bounds.width, subviews: subviews) {
            var x = bounds.minX
            for index in row.indices {
                let size = fitted(subviews[index], width: bounds.width)
                subviews[index].place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
                x += size.width + spacing
            }
            y += row.height + spacing
        }
    }

    private struct Row {
        var indices: [Int] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    /// Its ideal size, no wider than a row: a long chip truncates.
    private func fitted(_ subview: LayoutSubview, width: CGFloat) -> CGSize {
        let size = subview.sizeThatFits(.unspecified)
        return CGSize(width: min(size.width, width), height: size.height)
    }

    private func arrange(width: CGFloat, subviews: Subviews) -> [Row] {
        subviews.indices.reduce(into: [Row]()) { rows, index in
            let size = fitted(subviews[index], width: width)
            if let last = rows.last, !last.indices.isEmpty, last.width + spacing + size.width <= width {
                rows[rows.count - 1].indices.append(index)
                rows[rows.count - 1].width += spacing + size.width
                rows[rows.count - 1].height = max(last.height, size.height)
            } else {
                rows.append(Row(indices: [index], width: size.width, height: size.height))
            }
        }
    }
}
