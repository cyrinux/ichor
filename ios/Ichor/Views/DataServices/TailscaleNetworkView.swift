import SwiftUI
import IchorCore

/// The network map of one address the Tailscale operator serves for `kind` (`"Service"` or
/// `"Ingress"`) `namespace`/`name`: the tailnet name (or, for an egress, the Service) through
/// each proxy to the pods and nodes, or out to the tailnet target. Like the Argo CD app map: a
/// tap on a box lights its path and shows its details; nothing is changed here.
struct TailscaleNetworkView: View {
    let kind: String
    let namespace: String
    let name: String
    let title: String

    @Environment(AppModel.self) private var app
    @State private var state: LoadState<NetMap> = .loading
    @State private var selected: String?
    @State private var presented: String?
    @State private var expanded = false

    var body: some View {
        LoadStateView(state: state, retry: load) { network in
            content(network)
        }
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        guard let client = app.client else { return }
        let loaded: LoadState<NetMap> = await .from {
            try await client.tailscaleNetwork(kind: kind, namespace: namespace, name: name)
        }
        state = state.refreshed(with: loaded)
    }

    @ViewBuilder
    private func content(_ network: NetMap) -> some View {
        if network.isEmpty {
            Label("The operator does not serve this object.", systemImage: "network.slash")
                .font(.callout)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding()
        } else {
            List {
                if let problem = network.problem {
                    Section {
                        Label {
                            Text(verbatim: [problem.kind, problem.namespace, problem.name, problem.detail]
                                .filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.callout).textSelection(.enabled)
                        } icon: {
                            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.red)
                        }
                    }
                }
                Section {
                    NetMapGraph(
                        network: network, selected: selected, presented: $presented, expanded: $expanded,
                        tap: tap, clear: clear
                    ) { node in
                        ArgoNetDetails(
                            node: node, nodeRef: nil, pod: nil,
                            openNode: { _ in }, delete: { _ in nil })
                    }
                    .listRowInsets(EdgeInsets())
                } footer: {
                    legend
                }
            }
        }
    }

    private func tap(_ node: NetMapNode) {
        withAnimation(.snappy) {
            if selected == node.id {
                clear()
            } else {
                selected = node.id
                presented = node.id
            }
        }
    }

    private func clear() {
        withAnimation(.snappy) {
            selected = nil
            presented = nil
        }
    }

    private var legend: some View {
        HStack(spacing: 14) {
            legendItem(String(localized: "Serving"), color: .green, dashed: false)
            legendItem(String(localized: "Degraded"), color: attentionColor, dashed: false)
            legendItem(String(localized: "Broken"), color: .red, dashed: true)
        }
        .font(.caption2)
        .accessibilityElement(children: .combine)
    }

    private func legendItem(_ label: String, color: Color, dashed: Bool) -> some View {
        HStack(spacing: 5) {
            Path { line in
                line.move(to: CGPoint(x: 1, y: 2))
                line.addLine(to: CGPoint(x: 17, y: 2))
            }
            .stroke(color, style: StrokeStyle(lineWidth: 2, lineCap: .round, dash: dashed ? [3, 3] : []))
            .frame(width: 18, height: 4)
            Text(verbatim: label)
        }
    }
}
