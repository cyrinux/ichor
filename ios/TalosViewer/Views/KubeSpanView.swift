import SwiftUI
import TalosViewerCore

/// KubeSpan peers per node. Only peer statuses are read: the identity resource holds the
/// WireGuard private key and is never requested.
struct KubeSpanView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeSpanOverview> = .loading
    @State private var hostnames: [String: String] = [:]

    var body: some View {
        LoadStateView(state: state, retry: load) { overview in
            List {
                let down = overview.nodes.reduce(0) { $0 + $1.down }
                Section {
                    Text("KubeSpan on \(overview.nodes.filter(\.enabled).count) of \(overview.nodes.count) nodes" + (down > 0 ? " · \(down) peer link(s) down" : " · no link down"))
                        .foregroundStyle(down > 0 ? .red : .secondary)
                }
                ForEach(overview.nodes) { node in
                    Section {
                        if let error = node.error { Text(error).font(.caption).foregroundStyle(.red) }
                        ForEach(node.peers) { PeerRow(peer: $0) }
                    } header: {
                        HStack {
                            Text(hostnames[node.node] ?? node.node)
                            Spacer()
                            if node.error != nil {
                                StatusPill(label: "Unreachable", color: .red)
                            } else if !node.enabled {
                                StatusPill(label: "Off", color: .gray)
                            } else if node.down > 0 {
                                StatusPill(label: "\(node.down) down", color: .red)
                            } else {
                                StatusPill(label: "\(node.up)/\(node.peers.count) up", color: .green)
                            }
                        }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle("KubeSpan")
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        if let overview = try? await client.overview() {
            hostnames = Dictionary(overview.nodes.map { ($0.node, $0.hostname) }, uniquingKeysWith: { a, _ in a })
        }
        state = await .from { try await client.kubespan() }
    }
}

private struct PeerRow: View {
    let peer: KubeSpanPeer

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(peer.label.isEmpty ? String(peer.publicKey.prefix(12)) : peer.label)
                Spacer()
                switch peer.state {
                case "up": StatusPill(label: "Up", color: .green)
                case "down": StatusPill(label: "Down", color: .red)
                default: StatusPill(label: "Unknown", color: .gray)
                }
            }
            Text(details).font(.caption).foregroundStyle(.secondary)
        }
    }

    private var details: String {
        var parts: [String] = []
        if !peer.endpoint.isEmpty { parts.append(peer.endpoint) }
        if peer.lastHandshake > 0 {
            parts.append("handshake \(formatDuration(Int64(Date().timeIntervalSince1970) - peer.lastHandshake)) ago")
        }
        parts.append("↓ \(formatBytes(peer.rx)) ↑ \(formatBytes(peer.tx))")
        return parts.joined(separator: "  ·  ")
    }
}
