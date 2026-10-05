import SwiftUI
import IchorCore

/// The KubeSpan map tab: a one-line summary, the network test pick bar, the map, its legend,
/// and a sheet for a tapped link. Two nodes picked on the map, or a link, open the network test
/// between them. Same as Android's TopologyContent.
struct KubeSpanMapTab: View {
    let topology: ClusterTopology
    let netPerf: NetPerfSession
    let refresh: () async -> Void
    let openNode: (TopologyNode) -> Void

    /// Index in `topology.links` of the link whose details are open.
    @State private var link: Int?
    @State private var picking = false
    @State private var picked: [String] = []
    @State private var testing = false

    var body: some View {
        // Node names of the network test are Kubernetes node names: the hostnames on Talos.
        let tests = Dictionary(uniqueKeysWithValues: topology.links.indices.compactMap { i in
            netPerf.history.latestBetween(topology.links[i].a, topology.links[i].b).map { (i, $0) }
        })
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                summary
                if topology.nodes.isEmpty {
                    Text("Nothing to map: no cluster member or KubeSpan peer was found.")
                } else {
                    NetPerfPickBar(picking: picking, picked: picked.count, running: netPerf.isRunning,
                                   toggle: { picking.toggle(); picked = [] },
                                   open: { testing = true })
                    TopologyMapView(
                        topology: topology,
                        speeds: tests.compactMapValues { $0.podThroughputMbps.map(formatMbps) },
                        picked: picked,
                        selected: link,
                        onNode: tap,
                        onLink: { link = $0 }
                    )
                    TopologyLegend()
                    if topology.nodes.contains(where: { !$0.queried }) {
                        Text("Nodes in grey are not in this talosconfig: they are drawn from what their peers report.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .padding()
        }
        .refreshable { await refresh() }
        .themedBackground()
        .sheet(isPresented: Binding(get: { link != nil }, set: { if !$0 { link = nil } })) {
            if let index = link, topology.links.indices.contains(index) {
                let shown = topology.links[index]
                TopologyLinkSheet(
                    link: shown,
                    names: names,
                    test: tests[index],
                    onTest: netPerf.isRunning ? nil : { test(client: shown.a, server: shown.b) }
                )
            }
        }
        .sheet(isPresented: $testing) {
            NavigationStack {
                NetPerfView(session: netPerf)
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) { Button("Done") { testing = false } }
                    }
            }
        }
    }

    private var names: [String: String] {
        Dictionary(topology.nodes.map { ($0.id, $0.hostname.isEmpty ? $0.id : $0.hostname) }, uniquingKeysWith: { a, _ in a })
    }

    private var summary: some View {
        let broken = topology.links.filter(\.isBroken).count
        let parts = [
            String(localized: "\(topology.sites.count) sites"),
            String(localized: "\(topology.links.count) links"),
            broken > 0 ? String(localized: "\(broken) peer links down") : String(localized: "no link down"),
        ]
        return Text(verbatim: parts.joined(separator: " · "))
            .font(.subheadline)
            .foregroundStyle(broken > 0 ? Color.statusBad : .secondary)
    }

    private func tap(_ node: TopologyNode) {
        guard picking else { return openNode(node) }
        picked = picked.pickingNode(node.id)
        if picked.count == 2 { test(client: picked[0], server: picked[1]) }
    }

    /// Opens the network test sheet set up from `client` to `server`.
    private func test(client: String, server: String) {
        netPerf.prepare(client: client, server: server)
        picking = false
        picked = []
        link = nil
        testing = true
    }
}

extension NetPerfSession {
    /// The setup from `client` to `server`, back from a finished or saved test; checked against
    /// the ready nodes once the test screen loads them. Nothing changes while a test runs.
    func prepare(client: String, server: String) {
        guard !isRunning else { return }
        reset()
        viewing = nil
        setup = setup.between(client: client, server: server, nodes: nil)
    }
}

/// Above the map: the button that turns node taps into picking a pair for a network test, with
/// what to tap next; while a test runs, a button that brings it back.
private struct NetPerfPickBar: View {
    let picking: Bool
    let picked: Int
    let running: Bool
    let toggle: () -> Void
    let open: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            if running {
                Button(action: open) {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.small)
                        Text("Test running…")
                    }
                }
                .buttonStyle(.bordered)
            } else if picking {
                Button(action: toggle) { Label("Test speed", systemImage: "speedometer") }
                    .buttonStyle(.borderedProminent)
                    .accessibilityAddTraits(.isSelected)
                Text(picked == 0 ? LocalizedStringKey("Tap the client node: it sends.") : LocalizedStringKey("Now tap the server node."))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                Button(action: toggle) { Label("Test speed", systemImage: "speedometer") }
                    .buttonStyle(.bordered)
            }
        }
        .controlSize(.small)
    }
}
