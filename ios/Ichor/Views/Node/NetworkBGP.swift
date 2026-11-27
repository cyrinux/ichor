import IchorCore
import SwiftUI

/// The node's own BGP sessions (Talos 1.14+ native BGP); hidden on older nodes and on nodes that
/// do not peer. A peer restarting under graceful restart keeps its routes: shown as such, not down.
struct BGPSection: View {
    let network: NodeNetwork

    var body: some View {
        if network.showsBGP {
            Section {
                SectionError(message: network.errors["bgp"])
                ForEach(network.bgpPeers) { BGPPeerRow(peer: $0) }
            } header: {
                Text(verbatim: "BGP")
            } footer: {
                Text(
                    "The node's own BGP sessions (Talos native BGP): each peer, its AS number, the session state and the prefixes exchanged."
                )
            }
        }
    }
}

/// One session: the peer and its state, the AS numbers, the prefixes in and out, how long it is up.
struct BGPPeerRow: View {
    let peer: BGPPeer

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: peer.peer.isEmpty ? "?" : peer.peer)
                    .font(.headline.monospaced())
                    .lineLimit(1)
                Spacer()
                StatusPill(label: peer.state.lowercased().replacingOccurrences(of: "_", with: " "), color: stateColor)
            }
            Text(verbatim: asLine).font(.caption).foregroundStyle(.secondary)
            Text(verbatim: prefixLine).font(.caption).foregroundStyle(.secondary)
            if peer.peerRestarting || peer.gracefulRestart {
                Text(verbatim: tags).font(.caption.monospaced())
            }
        }
    }

    private var stateColor: Color {
        if peer.established { return .statusOK }
        return peer.peerRestarting ? .statusWarn : .statusBad
    }

    /// "AS 64512 · local AS 64601 · instance fabric".
    private var asLine: String {
        let (remote, local) = (String(peer.peerAsn), String(peer.localAsn))
        let asn = String(localized: "AS \(remote) · local AS \(local)")
        guard !peer.instance.isEmpty else { return asn }
        let instance = peer.instance
        return "\(asn)  ·  \(String(localized: "instance \(instance)"))"
    }

    /// "12 in · 3 out · established 3 hours ago".
    private var prefixLine: String {
        let (received, advertised) = (String(peer.received), String(peer.advertised))
        let prefixes = String(localized: "\(received) in · \(advertised) out")
        guard peer.established, let since = peer.sinceDate else { return prefixes }
        let ago = RelativeDateTimeFormatter().localizedString(for: since, relativeTo: .now)
        return "\(prefixes)  ·  \(String(localized: "established \(ago)"))"
    }

    private var tags: String {
        [
            peer.peerRestarting ? String(localized: "peer restarting") : nil,
            peer.gracefulRestart ? String(localized: "graceful restart") : nil,
        ]
        .compactMap { $0 }
        .joined(separator: "  ·  ")
    }
}
