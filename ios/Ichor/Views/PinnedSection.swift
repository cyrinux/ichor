import SwiftUI
import IchorCore

/// The overview's Pinned card: the nodes, workloads and GitOps apps pinned on this cluster, each
/// opening its screen in one tap (as its share link would), with its state when the overview
/// already holds it: ready for a node, health and sync for an app. A swipe unpins. Rendered
/// only with pins (OverviewCard.pinned is absent without).
struct PinnedSection: View {
    let pins: [Pin]
    let nodes: [NodeOverview]
    /// nil when not loaded or the role cannot read them: no chip, the pin stays.
    let argo: ArgoStatus?
    let flux: FluxStatus?
    /// Screenshot mode: the labels are the names as they were pinned, so only the kind shows.
    let masked: Bool
    let open: (Pin) -> Void
    let unpin: (Pin) -> Void

    var body: some View {
        Section {
            ForEach(pins) { pin in
                Button { open(pin) } label: {
                    PinnedRow(pin: pin, masked: masked, chip: chip(pin))
                }
                .foregroundStyle(.primary)
                .swipeActions {
                    Button(role: .destructive) { unpin(pin) } label: { Label("Unpin", systemImage: "pin.slash") }
                }
                .contextMenu {
                    Button(role: .destructive) { unpin(pin) } label: { Label("Unpin", systemImage: "pin.slash") }
                }
            }
        } header: {
            Text("Pinned")
        }
    }

    private func chip(_ pin: Pin) -> PinChip? {
        let target = pin.target
        switch target.target {
        case .node:
            guard let node = pinnedNode(target, in: nodes) else { return nil }
            return PinChip(text: node.status.label, color: node.status.color)
        case .argoApp:
            guard let app = argo?.app(namespace: target.namespace, name: target.name) else { return nil }
            let color = app.health == .healthy && app.sync == .outOfSync ? app.sync.color : app.health.color
            return PinChip(text: "\(app.health.label) · \(app.sync.label)", color: color)
        case .fluxApp:
            guard let app = flux?.app(kind: target.kind, namespace: target.namespace, name: target.name) else { return nil }
            return PinChip(text: app.state.label, color: app.state.color)
        default:
            return nil
        }
    }
}

/// The overview's node a pin names: by address, else by hostname.
func pinnedNode(_ target: ShareTarget, in nodes: [NodeOverview]) -> NodeOverview? {
    nodes.first { !target.addr.isEmpty && $0.node == target.addr }
        ?? nodes.first { !target.host.isEmpty && $0.hostname == target.host }
}

private struct PinChip {
    let text: String
    let color: Color
}

private struct PinnedRow: View {
    let pin: Pin
    let masked: Bool
    let chip: PinChip?

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: pin.target.pinSymbol)
                .foregroundStyle(.secondary)
                .frame(width: 24)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                if masked {
                    pin.target.pinKind
                } else {
                    Text(verbatim: pin.label).lineLimit(1)
                    if !pin.target.namespace.isEmpty {
                        Text(verbatim: pin.target.namespace).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
            }
            Spacer(minLength: 8)
            if let chip {
                Text(verbatim: chip.text)
                    .font(.caption)
                    .lineLimit(1)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(chip.color.opacity(0.16), in: Capsule())
                    .foregroundStyle(chip.color)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

extension ShareTarget {
    /// The SF Symbol of a pinned screen's kind.
    var pinSymbol: String {
        switch target {
        case .node: "server.rack"
        case .workload: "square.stack.3d.up"
        case .pod: "cube"
        case .cronJob: "clock.arrow.circlepath"
        case .argoApp, .fluxApp: "arrow.triangle.2.circlepath"
        default: "pin"
        }
    }

    /// The kind of a pinned screen, shown in place of its name in screenshot mode. Kubernetes
    /// kinds and product names are never translated.
    var pinKind: Text {
        switch target {
        case .node: Text("Node")
        case .workload: Text(verbatim: kind.isEmpty ? "Workload" : kind)
        case .pod: Text(verbatim: "Pod")
        case .cronJob: Text(verbatim: "CronJob")
        case .argoApp: Text(verbatim: "Argo CD")
        case .fluxApp: Text(verbatim: kind.isEmpty ? "Flux" : kind)
        default: Text("Pinned")
        }
    }
}

/// "Pin" / "Unpin" next to a screen's share link: adds it to (or takes it off) the Pinned card
/// of the cluster on screen. Not offered in screenshot mode (the names on screen are masked, a
/// pin must name the real screen), on a cluster added from a kubeconfig (no Talos overview to
/// hold the card) or without a cluster fingerprint.
struct PinButton: View {
    let target: ShareTarget
    /// The name the card shows: the node's hostname, the object's name.
    let label: String

    @Environment(AppModel.self) private var model

    var body: some View {
        if let fingerprint, Pin.pinnable.contains(target.target) {
            let store = PinStore.shared
            let pins = store.pins(for: fingerprint)
            if pins.contains(target) {
                Button { store.remove(target, fingerprint: fingerprint) } label: {
                    Label("Unpin", systemImage: "pin.slash")
                }
            } else if pins.isFull {
                // A menu item cannot explain itself once tapped: the limit is its title.
                Button {} label: {
                    Label(String(localized: "\(PinList.limit) pins at most. Unpin one first."), systemImage: "pin")
                }
                .disabled(true)
            } else {
                Button { store.toggle(Pin(target: target, label: label), fingerprint: fingerprint) } label: {
                    Label("Pin", systemImage: "pin")
                }
            }
        }
    }

    private var fingerprint: String? {
        guard !model.privacyMask, let context = model.activeSummary, !context.isKube, !context.fingerprint.isEmpty else { return nil }
        return context.fingerprint
    }
}
