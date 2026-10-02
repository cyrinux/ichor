import SwiftUI
import IchorCore

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
                    Text(summary(overview, down: down))
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
                                StatusPill(label: String(localized: "Unreachable"), color: .red)
                            } else if !node.enabled {
                                StatusPill(label: String(localized: "Off"), color: .gray)
                            } else if node.down > 0 {
                                StatusPill(label: String(localized: "\(node.down) down"), color: .red)
                            } else {
                                StatusPill(label: String(localized: "\(node.up)/\(node.peers.count) up"), color: .green)
                            }
                        }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(Text(verbatim: "KubeSpan"))
        .task { await load() }
    }

    private func summary(_ overview: KubeSpanOverview, down: Int) -> String {
        let enabled = String(localized: "KubeSpan on \(overview.nodes.filter(\.enabled).count) of \(overview.nodes.count) nodes")
        let links = down > 0 ? String(localized: "\(down) peer links down") : String(localized: "no link down")
        return "\(enabled) · \(links)"
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
                case "up": StatusPill(label: String(localized: "Up"), color: .green)
                case "down": StatusPill(label: String(localized: "Down"), color: .red)
                default: StatusPill(label: String(localized: "Unknown"), color: .gray)
                }
            }
            Text(details).font(.caption).foregroundStyle(.secondary)
        }
    }

    private var details: String {
        var parts: [String] = []
        if !peer.endpoint.isEmpty { parts.append(peer.endpoint) }
        if peer.lastHandshake > 0 {
            let age = localizedDuration(Int64(Date().timeIntervalSince1970) - peer.lastHandshake)
            parts.append(String(localized: "handshake \(age) ago"))
        }
        parts.append("↓ \(formatBytes(peer.rx)) ↑ \(formatBytes(peer.tx))")
        return parts.joined(separator: "  ·  ")
    }
}
