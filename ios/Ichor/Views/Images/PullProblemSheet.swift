import SwiftUI
import IchorCore

/// Why a pod's image cannot be pulled: the cause in plain words, what to check, the kubelet's
/// own message (selectable), and the screen that helps when the pod's node is a Talos node of
/// this cluster (the registry mirror snippet, or the node's network tools).
struct PullProblemSheet: View {
    let pod: KubePod
    let pull: PullProblem

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    /// The pod's node among the cluster's Talos nodes; nil until found, or when it is none.
    @State private var node: NodeOverview?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(pull.title.text)
                        .font(.headline)
                    Text(pull.hint.text)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
                if !pod.waiting.isEmpty {
                    Section {
                        Text(verbatim: pod.waiting)
                            .font(.caption.monospaced())
                            .textSelection(.enabled)
                    }
                }
                if let action = pull.action, let node {
                    Section { link(action, node: node) }
                }
            }
            .navigationTitle("Why the image cannot be pulled")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .task { await findNode() }
        }
    }

    @ViewBuilder
    private func link(_ action: PullAction, node: NodeOverview) -> some View {
        switch action {
        case .registryMirror(let host):
            NavigationLink {
                MachineConfigView(
                    node: node.node, hostname: node.hostname,
                    snippet: SnippetRequest(id: "registry_mirror", params: host.isEmpty ? [:] : ["host": host]))
            } label: {
                Label("Registry mirror snippet", systemImage: "wand.and.stars")
            }
        case .netTools:
            NavigationLink {
                NetToolsView(node: node.node, hostname: node.hostname)
            } label: {
                Label("Network tools on \(node.hostname)", systemImage: "network")
            }
        }
    }

    /// The Talos node the pod runs on, by hostname: only asked when a link needs it, never on a
    /// cluster added from a kubeconfig (no Talos API to point the screens at).
    private func findNode() async {
        guard pull.action != nil, !pod.node.isEmpty, !model.activeIsKube, let client = model.client else { return }
        let nodes = (try? await client.overview().nodes) ?? []
        node = nodes.first { $0.reachable && $0.hostname == pod.node }
    }
}

extension PullTitle {
    var text: String {
        switch self {
        case .notFound(let registry): String(localized: "Image not found on \(registry)")
        case .unauthorized(let registry): String(localized: "\(registry) refused the pull: check the pull secret")
        case .rateLimited(let registry): String(localized: "\(registry) pull limit reached")
        case .tls(let registry): String(localized: "\(registry) certificate not trusted")
        case .network(let registry): String(localized: "Node cannot reach \(registry)")
        case .platform: String(localized: "No image for this node's CPU architecture")
        case .invalidName: String(localized: "Invalid image name")
        case .unknown(let registry): String(localized: "Image cannot be pulled from \(registry)")
        }
    }
}

extension PullHint {
    var text: String {
        switch self {
        case .notFound: String(localized: "Check the image name and tag: the registry has no such image or tag.")
        case .secrets(let names):
            String(localized: "The pod's pull secrets: \(names). One must be for this registry and still valid.")
        case .noSecret: String(localized: "The pod has no pull secret: a private image needs one in imagePullSecrets.")
        case .rateLimited:
            String(
                localized:
                    "Anonymous pulls from this registry are limited. A pull-through mirror or registry credentials lift the limit."
            )
        case .tls:
            String(
                localized:
                    "The registry's certificate is not signed by an authority the node trusts: add its CA to the machine config, or fix the certificate."
            )
        case .network:
            String(
                localized:
                    "The node could not resolve or reach the registry: check its DNS, routes and egress, or use a mirror it can reach."
            )
        case .platform:
            String(
                localized:
                    "The image has no build for this node's architecture: pick a multi-arch tag or schedule the pod on matching nodes."
            )
        case .invalidName:
            String(localized: "The image reference cannot be parsed: check its spelling (lower case, no spaces).")
        case .unknown: String(localized: "The kubelet's message below says what went wrong.")
        }
    }
}
