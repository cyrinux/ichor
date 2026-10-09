import SwiftUI
import IchorCore

/// A node's full row in a list (the overview, the Nodes screen). Swipe right: live graphs. Swipe
/// left: reboot and shut down (each only opens its confirmation), shell, logs. Long press:
/// everything, plus Copy IP.
struct NodeListRow: View {
    let node: NodeOverview
    /// The version every node runs, which the summary shows: left out of the row.
    let sharedVersion: String?
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model

    var body: some View {
        let ref = node.ref
        let row = NodeOverviewRow(node: node, publicIPs: node.shownPublicIPs(probed: model.activePublicIPs), sharedVersion: sharedVersion)
        Group {
            if node.reachable {
                NavigationLink(value: Route.node(ref)) { row }
            } else {
                row
            }
        }
        .swipeActions(edge: .leading) {
            if node.reachable {
                Button { path.append(.nodeLive(ref)) } label: { Label("Live", systemImage: "chart.xyaxis.line") }
                    .tint(.blue)
            }
        }
        .swipeActions(edge: .trailing) {
            if node.reachable {
                if model.allows(.power) {
                    Button { path.append(.nodePower(ref, .reboot)) } label: { Label("Reboot", systemImage: "power") }
                        .tint(.red)
                    Button { path.append(.nodePower(ref, .shutdown)) } label: { Label("Shut down", systemImage: "power") }
                        .tint(.orange)
                }
                if model.allows(.debugShell) {
                    Button { path.append(.debugShell(node: node.node, hostname: node.hostname)) } label: {
                        Label("Shell", systemImage: "apple.terminal")
                    }
                    .tint(.indigo)
                }
                Button { path.append(.logs(node: node.node, hostname: node.hostname, service: nil)) } label: {
                    Label("Logs", systemImage: "text.alignleft")
                }
            } else if let targets = wakeTargets, !targets.isEmpty {
                // Down: the phone can still power it on over the LAN.
                Button { WakeOnLanCenter.shared.wake(node, targets: targets) } label: { Label("Wake (Wake-on-LAN)", systemImage: "power.circle") }
                    .tint(.green)
            }
        }
        .contextMenu { NodeMenu(node: node, path: $path) }
        // VoiceOver already lists the swipe actions; this one is only in the context menu.
        .accessibilityActions {
            Button("Copy IP") { UIPasteboard.general.string = node.node }
        }
    }

    /// Where a wake sends magic packets; nil in screenshot mode.
    private var wakeTargets: [WolTarget]? {
        model.wakeOnLanFingerprint.map { WakeOnLanStore.shared.wakeTargets(fingerprint: $0, node: node.node) }
    }
}

/// Everything a node offers, for a row's, a chip's or a dot's long press.
struct NodeMenu: View {
    let node: NodeOverview
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model

    var body: some View {
        let ref = node.ref
        if node.reachable {
            Button { path.append(.nodeLive(ref)) } label: { Label("Live graphs", systemImage: "chart.xyaxis.line") }
            Button { path.append(.node(ref)) } label: { Label("Services and logs", systemImage: "list.bullet") }
            Button { path.append(.logs(node: node.node, hostname: node.hostname, service: nil)) } label: {
                Label("Kernel log", systemImage: "text.alignleft")
            }
            if model.allows(.debugShell) {
                Button { path.append(.debugShell(node: node.node, hostname: node.hostname)) } label: {
                    Label("Debug shell", systemImage: "apple.terminal")
                }
            }
            // Through the Kubernetes API (os:admin, like the workloads).
            if model.allows(.workloads) {
                Button { path.append(.drain(node: node.node, hostname: node.hostname)) } label: {
                    Label("Drain…", systemImage: "rectangle.portrait.and.arrow.right")
                }
            }
            if model.allows(.power) {
                Button(role: .destructive) { path.append(.nodePower(ref, .reboot)) } label: { Label("Reboot…", systemImage: "power") }
                Button(role: .destructive) { path.append(.nodePower(ref, .shutdown)) } label: { Label("Shut down…", systemImage: "power") }
            }
        }
        // Needs no role: the phone sends the packet itself, the Talos API is not involved.
        WakeOnLanMenuItems(node: node)
        Button { UIPasteboard.general.string = node.node } label: { Label("Copy IP", systemImage: "doc.on.doc") }
    }
}

extension NodeOverview {
    /// What the node screens are opened with.
    var ref: NodeRef { NodeRef(address: node, hostname: hostname, role: role) }
}

/// A node as a full row: hostname, health, addresses, what it runs, its problems.
struct NodeOverviewRow: View {
    let node: NodeOverview
    /// What Talos knows, else what a probe found (see shownPublicIPs).
    var publicIPs: [String] = []
    /// The version every node runs, which the summary shows: left out of the row.
    var sharedVersion: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(node.hostname).font(.headline)
                Spacer()
                StatusPill(label: node.health.label, color: node.health.color)
            }
            // Below the pill rather than beside it: the full width keeps an IPv6 on one line.
            VStack(alignment: .leading, spacing: 2) {
                AddressLine(symbol: "network", address: node.node)
                if node.reachable && !publicIPs.isEmpty {
                    VStack(alignment: .leading, spacing: 2) {
                        ForEach(publicIPs, id: \.self) { AddressLine(symbol: "globe", address: $0) }
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(Text("Public IP: \(publicIPs.joined(separator: ", "))"))
                }
            }
            if node.reachable {
                Text([role, node.version == sharedVersion ? "" : node.version, node.stage, node.arch]
                    .filter { !$0.isEmpty }.joined(separator: "  ·  "))
                    .font(.caption)
            } else if let lastSeen = node.lastSeenDate {
                // Not answering, known from before: what it was, dimmed, and since when.
                Text([role, node.version, node.arch].filter { !$0.isEmpty }.joined(separator: "  ·  "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text("Last seen \(FreshnessFooter.ago(Date().timeIntervalSince(lastSeen)))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            ForEach(node.unmetConditions, id: \.self) {
                Text(verbatim: "\($0.name): \($0.reason)").font(.caption).foregroundStyle(.statusWarn)
            }
            if let error = node.error, !error.isEmpty {
                Text(error).font(.caption).foregroundStyle(.statusBad)
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    private var role: String {
        node.role == "controlplane" ? String(localized: "control plane") : node.role
    }
}

/// An address behind a fixed-width symbol, so private and public ones line up.
private struct AddressLine: View {
    let symbol: String
    let address: String

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: symbol).frame(width: 14)
            Text(verbatim: address)
        }
        .font(.caption2.monospaced())
        .foregroundStyle(.secondary)
    }
}
