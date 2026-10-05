import SwiftUI
import IchorCore

/// The cluster map: sites stacked as boxes (flag and zone or subnet in the header), their nodes
/// as chips, and the KubeSpan links between nodes coloured by state. Links across sites bow to
/// the right so they do not run over the sites in between. Same as Android's TopologyMap; the
/// geometry is IchorCore's topologyLayout.
struct TopologyMapView: View {
    let topology: ClusterTopology
    /// Throughput last measured on a link (by index) by the network test, shown at its middle.
    let speeds: [Int: String]
    /// Nodes picked for a network test: the client, then the server.
    let picked: [String]
    /// The link (index) whose details are open: drawn on top in the accent colour, its nodes outlined.
    let selected: Int?
    let onNode: (TopologyNode) -> Void
    let onLink: (Int) -> Void

    @State private var width: CGFloat = 0
    /// Rows grow with Dynamic Type, like the chips in them.
    @ScaledMetric(relativeTo: .caption) private var textScale: CGFloat = 100

    var body: some View {
        let layout = topologyLayout(topology, width: Double(width), textScale: Double(textScale) / 100)
        ZStack(alignment: .topLeading) {
            links(layout)
            ForEach(layout.sites, id: \.site.id) { box in
                Text(verbatim: box.site.title)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .frame(width: max(layout.width - 24, 0), height: layout.header, alignment: .leading)
                    .offset(x: 12, y: box.top)
                    .accessibilityAddTraits(.isHeader)
            }
            ForEach(speeds.sorted { $0.key < $1.key }, id: \.key) { entry in
                let index = entry.key, speed = entry.value
                if topology.links.indices.contains(index), let middle = layout.middle(of: topology.links[index], in: topology) {
                    Button { onLink(index) } label: {
                        Text(verbatim: speed)
                            .font(.caption2.weight(.medium))
                            .monospacedDigit()
                            .lineLimit(1)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8))
                    }
                    .buttonStyle(.plain)
                    .position(x: middle.x, y: middle.y)
                    .accessibilityLabel(Text(verbatim: "\(linkName(topology.links[index])): \(speed)"))
                }
            }
            ForEach(topology.nodes) { node in
                if let at = layout.nodes[node.id] {
                    TopologyNodeChip(
                        node: node,
                        broken: topology.brokenLinks(of: node.id),
                        pick: picked.firstIndex(of: node.id),
                        onSelected: selectedLink.map { node.id == $0.a || node.id == $0.b } ?? false,
                        action: { onNode(node) }
                    )
                    .frame(maxWidth: max(layout.cellWidth - 8, 44))
                    .position(x: at.x, y: at.y)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: layout.height)
        .background {
            GeometryReader { geometry in
                Color.clear
                    .onAppear { width = geometry.size.width }
                    .onChange(of: geometry.size.width) { _, new in width = new }
            }
        }
    }

    private var selectedLink: TopologyLink? {
        selected.flatMap { topology.links.indices.contains($0) ? topology.links[$0] : nil }
    }

    private func linkName(_ link: TopologyLink) -> String {
        let name = { (id: String) in
            topology.nodes.first { $0.id == id }.map { $0.hostname.isEmpty ? $0.id : $0.hostname } ?? id
        }
        return "\(name(link.a)) ↔ \(name(link.b))"
    }

    /// The site boxes and the links, under the chips; a tap near a link opens it. VoiceOver gets
    /// one action per link instead, since curves cannot be focused.
    private func links(_ layout: TopologyLayout) -> some View {
        let order = topology.links.indices.sorted { lhs, rhs in
            // Healthy links first so broken ones are drawn on top, and the selected one above all.
            let l = (lhs == selected ? 1 : 0, topology.links[lhs].isBroken ? 1 : 0)
            let r = (rhs == selected ? 1 : 0, topology.links[rhs].isBroken ? 1 : 0)
            return l < r
        }
        return Canvas { context, size in
            for box in layout.sites {
                let rect = Path(roundedRect: CGRect(x: 0, y: box.top, width: size.width, height: box.height), cornerRadius: 16)
                context.fill(rect, with: .color(Color(.tertiarySystemFill)))
                context.stroke(rect, with: .color(Color(.separator)), lineWidth: 1)
            }
            for index in order {
                let link = topology.links[index]
                guard let curve = layout.curve(of: link, in: topology) else { continue }
                var path = Path()
                path.move(to: CGPoint(x: curve.a.x, y: curve.a.y))
                path.addQuadCurve(to: CGPoint(x: curve.b.x, y: curve.b.y), control: CGPoint(x: curve.c.x, y: curve.c.y))
                if index == selected {
                    // A halo under the line in the accent colour, so it stands out whatever its state.
                    context.stroke(path, with: .color(.accentColor.opacity(0.35)), style: StrokeStyle(lineWidth: 10, lineCap: .round))
                    context.stroke(path, with: .color(.accentColor), style: StrokeStyle(lineWidth: 4, lineCap: .round))
                    continue
                }
                context.stroke(
                    path,
                    with: .color(linkColor(link.state).opacity(link.isBroken ? 1 : 0.55)),
                    style: StrokeStyle(lineWidth: link.isBroken ? 3 : 2, dash: link.state == "up" ? [] : [10, 6])
                )
            }
        }
        .frame(width: layout.width, height: layout.height)
        .contentShape(Rectangle())
        .onTapGesture(coordinateSpace: .local) { tap in
            if let index = layout.linkAt(topology, at: MapPoint(x: tap.x, y: tap.y), slop: 14) { onLink(index) }
        }
        .accessibilityElement()
        .accessibilityLabel(Text("Cluster map of sites, nodes and KubeSpan links"))
        .accessibilityActions {
            ForEach(topology.links.indices, id: \.self) { index in
                Button { onLink(index) } label: {
                    Text(verbatim: "\(linkName(topology.links[index])), \(linkStateLabel(topology.links[index].state))")
                }
            }
        }
    }
}

/// A link's colour by state: up, down, one end down, unknown.
func linkColor(_ state: String) -> Color {
    switch state {
    case "up": .statusOK
    case "down": .statusBad
    case "degraded": .statusWarn
    default: .gray
    }
}

func linkStateLabel(_ state: String) -> String {
    switch state {
    case "up": String(localized: "Up")
    case "down": String(localized: "Down")
    case "degraded": String(localized: "One end down")
    default: String(localized: "Unknown")
    }
}

/// A node on the map: a dot (filled for a control plane, a ring for a worker) coloured by
/// health, its hostname, and its zone, role, or part in the network test being picked.
private struct TopologyNodeChip: View {
    let node: TopologyNode
    /// Its links that are down or degraded.
    let broken: Int
    /// 0: picked as the client of a network test, 1: as the server.
    let pick: Int?
    /// At an end of the link whose details are open.
    let onSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                dot.frame(width: 10, height: 10)
                VStack(alignment: .leading, spacing: 0) {
                    Text(verbatim: name)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.primary)
                    Text(verbatim: detail)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                .lineLimit(1)
                .truncationMode(.tail)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .frame(minHeight: 44)
            .background(pickLabel != nil ? Color.accentColor.opacity(0.18) : Color(.systemBackground),
                        in: RoundedRectangle(cornerRadius: 12))
            .overlay {
                // Outlined when picked for a test or at an end of the selected link.
                if pickLabel != nil || onSelected {
                    RoundedRectangle(cornerRadius: 12).stroke(Color.accentColor, lineWidth: 2)
                }
            }
            .shadow(color: .black.opacity(0.12), radius: 1.5, y: 1)
            .contentShape(RoundedRectangle(cornerRadius: 12))
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: name))
        .accessibilityValue(Text(verbatim: accessibilityValue))
        .accessibilityAddTraits(.isButton)
    }

    private var name: String { node.hostname.isEmpty ? node.id : node.hostname }
    private var controlPlane: Bool { node.role == "controlplane" }
    private var role: String { controlPlane ? String(localized: "Control plane") : String(localized: "Worker") }

    private var zone: String? {
        guard !node.zone.isEmpty else { return nil }
        return [countryFlag(node.country), node.zone].filter { !$0.isEmpty }.joined(separator: " ")
    }

    private var pickLabel: String? {
        switch pick {
        case 0: String(localized: "Client")
        case 1: String(localized: "Server")
        default: nil
        }
    }

    private var detail: String { pickLabel ?? zone ?? role }

    private var accessibilityValue: String {
        var parts = [pickLabel, role, node.zone.isEmpty ? nil : node.zone].compactMap { $0 }
        if broken > 0 { parts.append(String(localized: "\(broken) peer links down")) }
        if let error = node.error { parts.append(error) }
        return parts.joined(separator: ", ")
    }

    private var color: Color {
        if node.error != nil { return .statusBad }
        if broken > 0 { return .statusWarn }
        return node.queried ? .statusOK : .gray
    }

    @ViewBuilder private var dot: some View {
        if controlPlane {
            Circle().fill(color)
        } else {
            Circle().inset(by: 1).stroke(color, lineWidth: 2)
        }
    }
}

/// What the line styles mean.
struct TopologyLegend: View {
    var body: some View {
        HStack(spacing: 16) {
            item(.statusOK, dashed: false, String(localized: "Up"))
            item(.statusWarn, dashed: true, String(localized: "One end down"))
            item(.statusBad, dashed: true, String(localized: "Down"))
        }
    }

    private func item(_ color: Color, dashed: Bool, _ label: String) -> some View {
        HStack(spacing: 6) {
            Canvas { context, size in
                var line = Path()
                line.move(to: CGPoint(x: 0, y: size.height / 2))
                line.addLine(to: CGPoint(x: size.width, y: size.height / 2))
                context.stroke(line, with: .color(color), style: StrokeStyle(lineWidth: 3, dash: dashed ? [5, 3] : []))
            }
            .frame(width: 22, height: 10)
            .accessibilityHidden(true)
            Text(verbatim: label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}
