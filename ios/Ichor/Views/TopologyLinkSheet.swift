import SwiftUI
import IchorCore

/// A tapped link of the KubeSpan map: each end's view of it, the last network test between its
/// two nodes, and a button that tests it. Same as Android's LinkDetails.
struct TopologyLinkSheet: View {
    let link: TopologyLink
    /// Node id -> hostname.
    let names: [String: String]
    let test: NetPerfReport?
    /// Opens the network test over this link; nil while one runs.
    let onTest: (() -> Void)?

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(Array(link.sides.enumerated()), id: \.offset) { _, side in
                        LinkSideRow(side: side, names: names)
                    }
                } footer: {
                    if link.sides.count < 2 {
                        Text("Only one end of this link was queried: the other node is not in this talosconfig.")
                    }
                }
                if let test {
                    Section("Last network test") { LinkTestRow(test: test) }
                }
                Section {
                    Button { onTest?() } label: {
                        Label("Test this link", systemImage: "speedometer")
                    }
                    .disabled(onTest == nil)
                }
            }
            .themedBackground()
            .navigationTitle(Text(verbatim: "\(name(link.a))  ↔  \(name(link.b))"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func name(_ id: String) -> String { names[id] ?? id }
}

/// One end's view of the link: its state, the endpoint it reaches the other end on, the last
/// handshake and the traffic.
private struct LinkSideRow: View {
    let side: TopologyLinkSide
    let names: [String: String]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("Seen from \(names[side.from] ?? side.from)")
                Spacer()
                LinkStatePill(state: side.state)
            }
            if !side.endpoint.isEmpty {
                Text(verbatim: side.endpoint).font(.caption.monospaced())
            }
            Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    private var details: String {
        var parts: [String] = []
        if side.lastHandshake > 0 {
            let age = localizedDuration(Int64(Date().timeIntervalSince1970) - side.lastHandshake)
            parts.append(String(localized: "handshake \(age) ago"))
        }
        parts.append("↓ \(formatBytes(side.rx)) ↑ \(formatBytes(side.tx))")
        return parts.joined(separator: "  ·  ")
    }
}

/// The last network test between the link's two nodes: direction, figures and when it ran.
private struct LinkTestRow: View {
    let test: NetPerfReport

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: "\(test.client) → \(test.server)").font(.subheadline.monospaced())
            Text(verbatim: figures).font(.caption).monospacedDigit()
            Text(verbatim: Date(epochMillis: test.started).formatted(date: .abbreviated, time: .shortened))
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    private var figures: String {
        let pod = test.results.filter { $0.path == NetPerfPath.pod && $0.error.isEmpty }
        let host = test.results.first {
            $0.path == NetPerfPath.host && $0.test == NetPerfTest.throughput && $0.error.isEmpty
        }
        return [
            test.podThroughputMbps.map(formatMbps),
            pod.first { $0.test == NetPerfTest.latency }?.latency.map { "p50 " + formatMicros($0.p50) },
            host.map { String(localized: "host to host \(formatMbps($0.throughputMbps))") },
        ].compactMap { $0 }.joined(separator: "  ·  ")
    }
}
