import SwiftUI
import IchorCore

/// What a tapped box of the network graph is, as a popover beside it: kind, name, namespace,
/// detail and health, then what can be done from there. A host opens in the browser, a node
/// opens its screen, a pod can be deleted so its controller starts a fresh one.
struct ArgoNetDetails: View {
    let node: ArgoNetNode
    /// The Talos node a Node box stands for, nil when unknown or unreachable.
    let nodeRef: NodeRef?
    /// The pod a Pod box stands for.
    let pod: KubePod?
    let openNode: (NodeRef) -> Void
    /// Deletes the pod: nil once done, else why not.
    let delete: (KubePod) async -> String?

    @Environment(\.openURL) private var openURL
    @State private var confirmDelete = false
    @State private var deleting = false
    @State private var failure: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                Image(systemName: node.kind.symbol)
                    .font(.title3)
                    .foregroundStyle(node.health.color)
                    .frame(width: 38, height: 38)
                    .background(node.health.color.opacity(0.15), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                VStack(alignment: .leading, spacing: 1) {
                    Text(verbatim: node.kind.rawValue).font(.caption).foregroundStyle(.secondary)
                    Text(verbatim: node.name).font(.headline.monospaced()).lineLimit(3).textSelection(.enabled)
                }
            }
            StatusPill(label: node.health.label, color: node.health.color)
            if !node.namespace.isEmpty {
                LabeledContent("Namespace") { Text(verbatim: node.namespace).font(.callout.monospaced()) }
            }
            if !node.detail.isEmpty {
                Text(verbatim: node.detail).font(.callout.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
            if node.shared {
                Label("Shared: not managed by this app", systemImage: "link").font(.caption).foregroundStyle(.secondary)
            }
            actions
            if let failure {
                Text(verbatim: failure).font(.caption).foregroundStyle(.statusBad)
            }
        }
        .padding()
        .frame(minWidth: 260, idealWidth: 300, maxWidth: 340, alignment: .leading)
        .presentationCompactAdaptation(.popover)
        .confirmationDialog(String(localized: "Delete pod \(node.name)?"), isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) { Task { await runDelete() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            if let owner = pod?.owner, !owner.isEmpty {
                Text("It is removed from \(node.namespace) after its grace period; \(owner) starts a new one.")
            } else {
                Text("It is removed from \(node.namespace) after its grace period; its controller, if any, starts a new one.")
            }
        }
    }

    @ViewBuilder private var actions: some View {
        if node.kind == .host, let url = URL(string: node.url), url.scheme == "https" || url.scheme == "http" {
            Button { openURL(url) } label: { Label("Open in browser", systemImage: "safari") }
                .buttonStyle(.borderedProminent)
        }
        if let nodeRef {
            Button { openNode(nodeRef) } label: { Label("Open node", systemImage: "cpu") }
                .buttonStyle(.borderedProminent)
        }
        if pod != nil {
            Button(role: .destructive) { confirmDelete = true } label: {
                HStack(spacing: 6) {
                    if deleting { ProgressView().controlSize(.small) } else { Image(systemName: "trash") }
                    Text("Delete pod…")
                }
            }
            .buttonStyle(.bordered)
            .disabled(deleting || node.detail.hasPrefix("Terminating"))
        }
    }

    private func runDelete() async {
        guard let pod, !deleting else { return }
        deleting = true
        defer { deleting = false }
        failure = await delete(pod)
    }
}
