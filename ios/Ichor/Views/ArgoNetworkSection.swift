import SwiftUI
import IchorCore

/// The network view of the app on screen, kept by ArgoAppView so it survives scrolling, read
/// again with the app. With nodes in it, the Talos nodes too, until once succeeds: a node box
/// opens its screen. Reads may overlap (a poll, a pull, the reload after a delete): only the
/// answer of the latest one started is kept.
@Observable
@MainActor
final class ArgoNetworkModel {
    private(set) var state: LoadState<ArgoNetwork> = .loading
    /// Reachable Talos nodes by hostname, which Kubernetes names them by.
    private(set) var nodeRefs: [String: NodeRef] = [:]
    private var key = ""
    /// The Talos nodes were read for key.
    private var knowsNodes = false
    private var readingNodes = false
    /// Counts the reads started, so an older answer never replaces a newer one.
    private var started = 0

    func load(with client: TalosClient, key: String, namespace: String, name: String) async {
        if key != self.key {
            self.key = key
            state = .loading
            nodeRefs = [:]
            knowsNodes = false
        }
        started += 1
        let ticket = started
        let loaded: LoadState<ArgoNetwork> = await .from { try await client.argoNetwork(namespace: namespace, name: name) }
        guard key == self.key, ticket == started else { return }
        state = state.refreshed(with: loaded)
        if case .loaded(let network, _, _) = state, network.nodes.contains(where: { $0.kind == .node }) {
            await loadNodes(with: client, key: key)
        }
    }

    /// The reachable Talos nodes by hostname; on failure, tried again with the next read.
    private func loadNodes(with client: TalosClient, key: String) async {
        guard !knowsNodes, !readingNodes else { return }
        readingNodes = true
        defer { readingNodes = false }
        guard let overview = try? await client.overview(), key == self.key else { return }
        let refs = overview.nodes.filter { $0.reachable && !$0.hostname.isEmpty }
            .map { ($0.hostname, NodeRef(address: $0.node, hostname: $0.hostname, role: $0.role)) }
        nodeRefs = Dictionary(refs) { first, _ in first }
        knowsNodes = true
    }
}

/// "Network": how traffic reaches the app, host to node, under the likely root cause when a
/// box is broken. A tap on a box lights its path and shows what it is, with what can be done
/// about it; another tap on it (or on the background) clears.
struct ArgoNetworkSection: View {
    let model: ArgoNetworkModel
    /// Hostnames Talos reports not ready: their boxes turn red.
    let downNodes: Set<String>
    /// The app's pods that are not ready, for the owner a deletion names.
    let pods: [KubePod]
    let openNode: (NodeRef) -> Void
    /// Something changed (a pod deleted): read again.
    let changed: () -> Void

    @Environment(AppModel.self) private var app
    @State private var selected: String?
    @State private var presented: String?
    @State private var expanded = false

    var body: some View {
        Section {
            switch model.state {
            case .loading:
                graph(ArgoNetwork.placeholder)
                    .redacted(reason: .placeholder)
                    .allowsHitTesting(false)
                    .listRowInsets(EdgeInsets())
            case .failed(let message):
                Text("Could not draw the network: \(message)").font(.callout).foregroundStyle(.secondary)
            case .loaded(let loaded, _, let refreshError):
                let network = loaded.marking(down: downNodes)
                if network.isEmpty {
                    Label("No Service: this app takes no traffic", systemImage: "network.slash")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                } else {
                    if let problem = network.problem { banner(problem, in: network) }
                    graph(network)
                        .listRowInsets(EdgeInsets())
                }
                if let refreshError {
                    Text(verbatim: refreshError).font(.caption).foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Network")
        } footer: {
            if case .loaded(let network, _, _) = model.state, !network.isEmpty { legend }
        }
    }

    private func graph(_ network: ArgoNetwork) -> some View {
        ArgoNetworkGraph(network: network, selected: selected, presented: $presented, expanded: $expanded,
                         tap: tap, clear: clear) { node in
            ArgoNetDetails(node: node, nodeRef: node.kind == .node ? model.nodeRefs[node.name] : nil,
                           pod: node.kind == .pod ? pod(node) : nil,
                           openNode: { ref in
                               presented = nil
                               openNode(ref)
                           },
                           delete: delete)
        }
    }

    private func tap(_ node: ArgoNetNode) {
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

    /// The pod a box stands for, with its owner when the app knows it.
    private func pod(_ node: ArgoNetNode) -> KubePod {
        pods.first { $0.namespace == node.namespace && $0.name == node.name } ?? KubePod(namespace: node.namespace, name: node.name)
    }

    /// nil once deleted, else why not.
    private func delete(_ pod: KubePod) async -> String? {
        guard let client = app.client else { return nil }
        do {
            try await client.deletePod(pod)
            selected = nil
            presented = nil
            changed()
            return nil
        } catch {
            return String(localized: "Could not delete \(pod.name): \(error.localizedDescription)")
        }
    }

    /// The likely root cause, in the likely-cause style of the conditions section.
    private func banner(_ problem: ArgoNetProblem, in network: ArgoNetwork) -> some View {
        let health = network.nodes.first { $0.kind == problem.kind && $0.name == problem.name }?.health ?? .critical
        return Label {
            Text(verbatim: problem.reason.text).font(.callout).textSelection(.enabled)
        } icon: {
            Image(systemName: health == .critical ? "exclamationmark.triangle.fill" : "exclamationmark.circle.fill")
                .foregroundStyle(health == .critical ? Color.red : attentionColor)
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

extension ArgoNetReason {
    /// The banner's sentence.
    var text: String {
        switch self {
        case .nodeNotReady(let name): String(localized: "Node \(name) is not ready: pods on it cannot serve traffic")
        case .nodeCordoned(let name): String(localized: "Node \(name) is cordoned: no new pods are scheduled on it")
        case .pod(let name, let detail): String(localized: "Pod \(name): \(detail)")
        case .serviceWithoutPods(let name): String(localized: "Service \(name) has no ready pods")
        case .noHealthyBackend(let kind, let name): String(localized: "\(kind) \(name) has no healthy backend")
        case .other(let kind, let name, let detail): [kind, name, detail].filter { !$0.isEmpty }.joined(separator: " ")
        }
    }
}

extension ArgoNetwork {
    /// What the section shows, redacted, while the first answer comes.
    static let placeholder: ArgoNetwork = {
        let host = ArgoNetNode(id: "h", layer: .entry, kind: .host, name: "app.example.com", detail: "https://app.example.com")
        let route = ArgoNetNode(id: "r", layer: .route, kind: .ingress, name: "app-ingress", detail: "10.0.0.1")
        let service = ArgoNetNode(id: "s", layer: .service, kind: .service, name: "app-service", detail: "ClusterIP 10.96.0.1")
        let pods = (1...2).map { ArgoNetNode(id: "p\($0)", layer: .pod, kind: .pod, name: "app-pod-\($0)", detail: "Running") }
        let node = ArgoNetNode(id: "n", layer: .node, kind: .node, name: "worker-node")
        let edges = [ArgoNetEdge(from: "h", to: "r"), ArgoNetEdge(from: "r", to: "s")] +
            pods.flatMap { [ArgoNetEdge(from: "s", to: $0.id), ArgoNetEdge(from: $0.id, to: "n")] }
        return ArgoNetwork(nodes: [host, route, service] + pods + [node], edges: edges)
    }()
}
