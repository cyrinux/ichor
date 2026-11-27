import IchorCore
import SwiftUI
import UIKit

/// The switches heard over LLDP (Talos 1.15+); hidden on older nodes and when none announced
/// itself. Every text comes from the network: plain text, never a link.
struct NeighborsSection: View {
    let network: NodeNetwork

    var body: some View {
        if network.showsNeighbors {
            Section {
                SectionError(message: network.errors["neighbors"])
                ForEach(network.neighbors) { NeighborRow(neighbor: $0) }
            } header: {
                Text("Neighbours")
            } footer: {
                Text("What the switches announce over LLDP: the switch and port at the other end of each cable.")
            }
        }
    }
}

/// "eth0 → sw-a", its port, VLANs and management addresses; a long press copies them.
struct NeighborRow: View {
    let neighbor: LLDPNeighbor

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: "\(neighbor.link) → \(neighbor.switchName)")
                .font(.headline.monospaced())
                .lineLimit(1)
            Text(verbatim: neighborPort(neighbor)).font(.caption).foregroundStyle(.secondary)
            if !neighbor.systemDescription.isEmpty {
                Text(verbatim: neighbor.systemDescription).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            if !vlans.isEmpty {
                Text(verbatim: vlans.joined(separator: "  ·  ")).font(.caption.monospaced())
            }
            ForEach(management, id: \.self) {
                Text(verbatim: $0).font(.caption.monospaced()).foregroundStyle(.secondary)
            }
        }
        .contextMenu {
            Button {
                UIPasteboard.general.string = copyText
            } label: {
                Label("Copy the neighbour details", systemImage: "doc.on.doc")
            }
        }
    }

    private var vlans: [String] {
        neighbor.vlans.map { String(format: String(localized: "VLAN %@"), $0.label) }
    }

    private var management: [String] {
        neighbor.managementAddresses.map { String(format: String(localized: "management %@"), $0) }
    }

    private var copyText: String {
        (["\(neighbor.link) → \(neighbor.switchName)", neighborPort(neighbor)] + vlans + management)
            .joined(separator: "\n")
    }
}

/// "port Ethernet12 · rack 3 uplink": the port id, then its description when it says more.
func neighborPort(_ neighbor: LLDPNeighbor) -> String {
    let port = portLabel(neighbor)
    guard !neighbor.portDescription.isEmpty, neighbor.portDescription != neighbor.portId else { return port }
    return "\(port) · \(neighbor.portDescription)"
}

/// "port Ethernet12" ("port ?" when the switch sent no port id).
private func portLabel(_ neighbor: LLDPNeighbor) -> String {
    String(format: String(localized: "port %@"), neighbor.portId.isEmpty ? "?" : neighbor.portId)
}

/// The switch and port each neighbour on a link announces, under the link's own row.
struct LinkNeighborsLine: View {
    let neighbors: [LLDPNeighbor]

    var body: some View {
        ForEach(neighbors) { n in
            Text(verbatim: "→ \(n.switchName) · \(portLabel(n))")
                .font(.caption.monospaced())
                .foregroundStyle(.tint)
                .lineLimit(1)
        }
    }
}
