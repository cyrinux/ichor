import SwiftUI
import IchorCore

/// KubeSpan: the cluster map (sites, nodes and the links between them) and the peers per node.
/// Only peer statuses are read: the identity resource holds the WireGuard private key and is
/// never requested. Same tabs as Android's KubeSpanScreen.
struct KubeSpanView: View {
    private enum Tab: Hashable { case map, peers }

    @Environment(AppModel.self) private var model
    @State private var tab = Tab.map
    @State private var map: LoadState<ClusterTopology> = .loading
    @State private var peers: LoadState<KubeSpanOverview> = .loading
    @State private var hostnames: [String: String] = [:]
    /// The network test started from the map: kept while a node screen is pushed on top.
    @State private var netPerf = NetPerfSession()
    @State private var openNode: NodeRef?

    var body: some View {
        VStack(spacing: 0) {
            Picker("View", selection: $tab) {
                Text("Map").tag(Tab.map)
                Text("Peers").tag(Tab.peers)
            }
            .pickerStyle(.segmented)
            .padding()

            switch tab {
            case .map:
                LoadStateView(state: map, retry: loadMap) { topology in
                    KubeSpanMapTab(topology: topology, netPerf: netPerf, refresh: loadMap, openNode: open)
                }
                .task { if case .loading = map { await loadMap() } }
            case .peers:
                LoadStateView(state: peers, retry: loadPeers) { overview in
                    KubeSpanPeersList(overview: overview, hostnames: hostnames)
                        .refreshable { await loadPeers() }
                        .themedBackground()
                }
                .task { if case .loading = peers { await loadPeers() } }
            }
        }
        .navigationTitle(Text(verbatim: "KubeSpan"))
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(item: $openNode) { NodeDetailView(ref: $0) }
        .task {
            if let cluster = model.activeSummary?.fingerprint {
                // The same saved tests as the network test screens: their speeds show on the links.
                netPerf.loadHistory(scope: "\(cluster)-\(model.privacyStorageKey)")
            }
        }
        // Leaving the screen stops a running test; opening a node from the map does not.
        .onDisappear { if openNode == nil, netPerf.isRunning { netPerf.leave() } }
    }

    /// Only a node the talosconfig targets has a detail screen.
    private func open(_ node: TopologyNode) {
        guard !node.node.trimmingCharacters(in: .whitespaces).isEmpty else { return }
        openNode = NodeRef(address: node.node, hostname: node.hostname.isEmpty ? node.id : node.hostname, role: node.role)
    }

    /// Reads the map; a fresh one also regroups the overview's nodes by site (TopologyStore).
    private func loadMap() async {
        guard let client = model.client else { return }
        let key = model.topologyKey
        if case .loading = map, let known = TopologyStore.shared.topology(for: key) {
            map = .loaded(known, at: Date())
        }
        let fetched: LoadState<ClusterTopology> = await .from { try await client.topology() }
        if case .loaded(let topology, _, _) = fetched { TopologyStore.shared.remember(topology, for: key) }
        map = map.refreshed(with: fetched)
    }

    private func loadPeers() async {
        guard let client = model.client else { return }
        if let overview = try? await client.overview() {
            hostnames = Dictionary(overview.nodes.map { ($0.node, $0.hostname) }, uniquingKeysWith: { a, _ in a })
        }
        peers = model.seeded(peers, from: .kubespan)
        peers = peers.refreshed(with: await .from { try await model.fetch(.kubespan, with: client) })
    }
}

/// The peers tab: KubeSpan peers per node.
private struct KubeSpanPeersList: View {
    let overview: KubeSpanOverview
    let hostnames: [String: String]

    var body: some View {
        List {
            let down = overview.nodes.reduce(0) { $0 + $1.down }
            Section {
                Text(summary(down: down))
                    .foregroundStyle(down > 0 ? .red : .secondary)
            }
            ForEach(overview.nodes) { node in
                Section {
                    if let error = node.error { Text(error).font(.caption).foregroundStyle(.statusBad) }
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
    }

    private func summary(down: Int) -> String {
        let enabled = String(localized: "KubeSpan on \(overview.nodes.filter(\.enabled).count) of \(overview.nodes.count) nodes")
        let links = down > 0 ? String(localized: "\(down) peer links down") : String(localized: "no link down")
        return "\(enabled) · \(links)"
    }
}

private struct PeerRow: View {
    let peer: KubeSpanPeer

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(peer.label.isEmpty ? String(peer.publicKey.prefix(12)) : peer.label)
                Spacer()
                LinkStatePill(state: peer.state)
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

/// A peer's or a link side's state: up, down or unknown.
struct LinkStatePill: View {
    let state: String

    var body: some View {
        switch state {
        case "up": StatusPill(label: String(localized: "Up"), color: .green)
        case "down": StatusPill(label: String(localized: "Down"), color: .red)
        default: StatusPill(label: String(localized: "Unknown"), color: .gray)
        }
    }
}
