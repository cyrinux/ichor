import SwiftUI
import IchorCore

// A network map, left to right: one column per layer with a box per host, Gateway, route,
// Service, pod and node (or any kind a caller draws in front), joined by curves where traffic
// flows (dashes run along the healthy hops, a broken hop stands still). Tapping a box lights its
// path. The Argo CD app's Network section draws one.

extension NetMapColumn {
    /// The column's title, by the kind of its boxes; a kind this table does not know is its own.
    var title: String {
        switch kind {
        case .host, .loadBalancer: String(localized: "Internet")
        case .gateway: String(localized: "Gateway")
        case .ingress, .httpRoute, .grpcRoute, .tlsRoute, .ingressRoute, .ingressRouteTCP: String(localized: "Routes")
        case .service: String(localized: "Services")
        case .pod: String(localized: "Pods")
        case .node: String(localized: "Nodes")
        case .unknown: kindName
        }
    }
}

extension NetMapKind {
    var symbol: String {
        switch self {
        case .host: "globe"
        case .loadBalancer: "point.3.connected.trianglepath.dotted"
        case .gateway: "network"
        case .ingress, .httpRoute, .grpcRoute, .tlsRoute, .ingressRoute, .ingressRouteTCP: "arrow.triangle.branch"
        case .service: "square.stack.3d.up"
        case .pod: "shippingbox"
        case .node: "cpu"
        case .unknown: "questionmark.square.dashed"
        }
    }
}

private enum NetMapMetrics {
    static let cardWidth: CGFloat = 136
    static let cardHeight: CGFloat = 52
    static let rowSpacing: CGFloat = 10
}

/// Each box's frame, by id, for the curves drawn behind them.
private struct NetMapAnchors: PreferenceKey {
    static var defaultValue: [String: Anchor<CGRect>] { [:] }

    static func reduce(value: inout [String: Anchor<CGRect>], nextValue: () -> [String: Anchor<CGRect>]) {
        value.merge(nextValue()) { $1 }
    }
}

struct NetMapGraph<Details: View>: View {
    let network: NetMap
    /// The tapped box, whose path stays lit; presented: the one showing its details.
    let selected: String?
    @Binding var presented: String?
    @Binding var expanded: Bool
    let tap: (NetMapNode) -> Void
    /// A tap beside the boxes.
    let clear: () -> Void
    @ViewBuilder let details: (NetMapNode) -> Details

    var body: some View {
        let columns = network.columns(expanded: expanded)
        let hidden = Set(columns.flatMap(\.hidden).map(\.id))
        let path = current.map { network.path(through: $0, hiding: hidden) }
        let rows = columns.map { $0.nodes.count + ($0.folds ? 1 : 0) }.max() ?? 1
        let height = CGFloat(rows) * NetMapMetrics.cardHeight + CGFloat(max(rows - 1, 0)) * NetMapMetrics.rowSpacing
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(alignment: .top, spacing: 40) {
                ForEach(columns) { column in
                    VStack(spacing: 10) {
                        Text(verbatim: column.title)
                            .font(.caption2.weight(.semibold))
                            .textCase(.uppercase)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                        VStack(spacing: NetMapMetrics.rowSpacing) {
                            ForEach(column.nodes) { node in card(node, path: path) }
                            if column.folds { foldCard(column, path: path) }
                        }
                        .frame(height: height)
                    }
                    .frame(width: NetMapMetrics.cardWidth)
                }
            }
            .backgroundPreferenceValue(NetMapAnchors.self) { anchors in
                GeometryReader { proxy in
                    NetMapEdges(
                        edges: network.edges(hiding: hidden), rects: anchors.mapValues { proxy[$0] }, path: path)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .contentShape(Rectangle())
            .onTapGesture(perform: clear)
        }
        .sensoryFeedback(.selection, trigger: selected)
        // The tapped box is gone (a pod replaced between two reads): nothing is selected any more.
        .onChange(of: current) { _, now in
            if now == nil && selected != nil { clear() }
        }
    }

    /// The selection, nil when its box is no longer in the graph: a stale one would dim it all.
    private var current: String? {
        selected.flatMap { network.node($0) == nil ? nil : $0 }
    }

    private func card(_ node: NetMapNode, path: NetMapPath?) -> some View {
        Button {
            tap(node)
        } label: {
            NetMapCard(
                symbol: node.kind.symbol, title: node.name,
                detail: node.detail.isEmpty ? node.kind.rawValue : node.detail,
                health: node.health, shared: node.shared, selected: current == node.id)
        }
        .buttonStyle(.plain)
        .opacity(lit(node.id, on: path) ? 1 : 0.25)
        .anchorPreference(key: NetMapAnchors.self, value: .bounds) { [node.id: $0] }
        .popover(
            isPresented: Binding(get: { presented == node.id }, set: { if !$0 { presented = nil } }),
            arrowEdge: .bottom
        ) { details(node) }
        .accessibilityLabel(
            Text(
                verbatim: [node.kind.rawValue, node.name, node.detail, node.health.label]
                    .filter { !$0.isEmpty }.joined(separator: ", "))
        )
        .accessibilityAddTraits(current == node.id ? .isSelected : [])
    }

    /// On the lit path, or nothing is selected.
    private func lit(_ id: String, on path: NetMapPath?) -> Bool {
        path?.contains(node: id) ?? true
    }

    /// "+N more" while a column is folded (it stands for its hidden boxes in the curves), "Show
    /// less" once open.
    private func foldCard(_ column: NetMapColumn, path: NetMapPath?) -> some View {
        let hidden = column.hidden
        let more = NetMap.moreID(layer: column.layer)
        return Button {
            withAnimation(.snappy) { expanded.toggle() }
        } label: {
            if hidden.isEmpty {
                NetMapCard(
                    symbol: "chevron.up", title: String(localized: "Show less"), detail: "", health: .idle,
                    shared: true, selected: false)
            } else {
                NetMapCard(
                    symbol: "ellipsis", title: String(localized: "+\(hidden.count) more"),
                    detail: column.kindName,
                    health: ServiceHealth.worst(hidden.map(\.health)), shared: true, selected: false)
            }
        }
        .buttonStyle(.plain)
        .opacity(hidden.isEmpty || lit(more, on: path) ? 1 : 0.25)
        .anchorPreference(key: NetMapAnchors.self, value: .bounds) {
            hidden.isEmpty ? [:] : [more: $0]
        }
    }
}

/// One box: a 3 pt bar in its health colour, a glyph, its name and a detail line. A shared
/// Gateway or route (not the app's own) has a dashed outline.
struct NetMapCard: View {
    let symbol: String
    let title: String
    let detail: String
    let health: ServiceHealth
    let shared: Bool
    let selected: Bool

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)
        HStack(spacing: 8) {
            Image(systemName: symbol)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(health.color)
                .frame(width: 18)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: title).font(.caption.weight(.semibold)).lineLimit(1).truncationMode(.middle)
                if !detail.isEmpty {
                    Text(verbatim: detail).font(.caption2.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.leading, 11)
        .padding(.trailing, 8)
        .frame(width: NetMapMetrics.cardWidth, height: NetMapMetrics.cardHeight)
        .background {
            shape.fill(Color(.tertiarySystemGroupedBackground))
            if health.needsAttention { shape.fill(health.color.opacity(0.08)) }
        }
        .overlay(alignment: .leading) {
            Rectangle().fill(health.color).frame(width: 3)
        }
        .clipShape(shape)
        .overlay {
            if selected {
                shape.strokeBorder(health.color, lineWidth: 2)
            } else if shared {
                shape.strokeBorder(Color.secondary.opacity(0.6), style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
            } else {
                shape.strokeBorder(Color(.separator).opacity(0.6), lineWidth: 0.5)
            }
        }
        .shadow(color: .black.opacity(0.06), radius: 2, y: 1)
        .contentShape(shape)
    }
}

extension ServiceHealth {
    /// A hop's colour: green where traffic flows, orange where part of it is lost, red where it stops.
    var edgeColor: Color {
        switch self {
        case .ok: .green
        case .warning: attentionColor
        case .critical: .red
        case .idle, .unknown: .secondary
        }
    }

    /// Traffic flows: the dashes move.
    var flows: Bool { self == .ok || self == .warning }
}

/// The curves, from each source's trailing middle to its target's leading middle, with dashes
/// running along them (still when Reduce Motion is on).
private struct NetMapEdges: View {
    let edges: [NetMapEdge]
    let rects: [String: CGRect]
    let path: NetMapPath?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let still = reduceMotion || !edges.contains { $0.health.flows }
        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: still)) { context in
            // 28 pt a second, wrapped on the dash pattern's 14 pt so the phase stays small.
            let phase =
                still ? 0 : (context.date.timeIntervalSinceReferenceDate * 28).truncatingRemainder(dividingBy: 14)
            Canvas { canvas, _ in
                for edge in edges {
                    guard let from = rects[edge.from], let to = rects[edge.to] else { continue }
                    draw(edge, curve: curve(from: from, to: to), phase: phase, in: &canvas)
                }
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private func curve(from: CGRect, to: CGRect) -> Path {
        let start = CGPoint(x: from.maxX, y: from.midY)
        let end = CGPoint(x: to.minX, y: to.midY)
        let pull = max((end.x - start.x) * 0.5, 12)
        var curve = Path()
        curve.move(to: start)
        curve.addCurve(
            to: end, control1: CGPoint(x: start.x + pull, y: start.y), control2: CGPoint(x: end.x - pull, y: end.y))
        return curve
    }

    private func draw(_ edge: NetMapEdge, curve: Path, phase: Double, in canvas: inout GraphicsContext) {
        let lit = path?.contains(edge: edge.id)
        canvas.opacity = lit == false ? 0.25 : 1
        let width: CGFloat = lit == true ? 2.5 : 2
        let color = edge.health.edgeColor
        if edge.health.flows {
            canvas.stroke(
                curve, with: .color(color.opacity(0.3)), style: StrokeStyle(lineWidth: width, lineCap: .round))
            canvas.stroke(
                curve, with: .color(color),
                style: StrokeStyle(lineWidth: width, lineCap: .round, dash: [5, 9], dashPhase: -phase))
        } else if edge.health == .critical {
            canvas.stroke(
                curve, with: .color(color), style: StrokeStyle(lineWidth: width, lineCap: .round, dash: [4, 4]))
        } else {
            canvas.stroke(curve, with: .color(color.opacity(0.6)), style: StrokeStyle(lineWidth: 1.5, lineCap: .round))
        }
    }
}
