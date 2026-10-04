import SwiftUI
import IchorCore

/// Every pod of the cluster with the status `kubectl get pods` shows, unhealthy ones first,
/// and a delete action so a controller starts a fresh one (os:admin).
struct PodsList: View {
    @Binding var namespace: String?
    let query: String
    /// Opens the pod's live flows; nil without Cilium.
    var onFlows: ((KubePod) -> Void)?

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[KubePod]> = .loading
    @State private var confirm: KubePod?
    @State private var deleting: Set<String> = []
    @State private var resultMessage: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { pods in
            let namespaces = podNamespaces(pods)
            let selected = namespace.flatMap { namespaces.contains($0) ? $0 : nil }
            let shown = filterPods(pods, namespace: selected, query: query)
            List {
                Section { NamespacePicker(namespaces: namespaces, namespace: $namespace) }
                Section {
                    ForEach(shown) { pod in
                        PodRow(pod: pod, showNamespace: selected == nil, deleting: deleting.contains(pod.id)) { confirm = pod }
                            .contextMenu {
                                if let onFlows {
                                    Button { onFlows(pod) } label: {
                                        Label("Live flows of this pod", systemImage: "point.3.filled.connected.trianglepath.dotted")
                                    }
                                }
                            }
                    }
                }
            }
            .overlay {
                if shown.isEmpty {
                    if query.isEmpty {
                        ContentUnavailableView("No pods", systemImage: "cube")
                    } else {
                        ContentUnavailableView.search(text: query)
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        .confirmationDialog(confirm.map { String(localized: "Delete pod \($0.name)?") } ?? "",
                            isPresented: $confirm.isPresent(),
                            titleVisibility: .visible,
                            presenting: confirm) { pod in
            Button("Delete", role: .destructive) {
                Task { await delete(pod) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { pod in
            if pod.owner.isEmpty {
                Text("It is removed from \(pod.namespace) after its grace period. No controller owns it: it will not come back.")
            } else {
                Text("It is removed from \(pod.namespace) after its grace period; \(pod.owner) starts a new one.")
            }
        }
        .messageAlert($resultMessage)
    }

    private func load() async {
        guard let client = model.client else { return }
        state = model.seeded(state, from: .pods, as: KubePodList.self) { $0.pods }
        state = state.refreshed(with: await .from { try await model.fetch(.pods, as: KubePodList.self, with: client).pods })
    }

    private func delete(_ pod: KubePod) async {
        guard let client = model.client, !deleting.contains(pod.id) else { return }
        deleting.insert(pod.id)
        defer { deleting.remove(pod.id) }
        do {
            try await client.deletePod(pod)
            resultMessage = String(localized: "\(pod.name) is being deleted")
            // Shows it terminating, and soon its replacement.
            await load()
        } catch {
            resultMessage = String(localized: "Could not delete \(pod.name): \(error.localizedDescription)")
        }
    }
}

private struct PodRow: View {
    let pod: KubePod
    let showNamespace: Bool
    let deleting: Bool
    let onDelete: () -> Void

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: pod.name)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(verbatim: [showNamespace ? pod.namespace : nil, pod.node.isEmpty ? nil : pod.node].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                HStack(spacing: 8) {
                    Text(verbatim: pod.status)
                        .font(.caption.monospaced())
                        .foregroundStyle(statusColor)
                    Text("\(pod.ready)/\(pod.containers) ready")
                        .foregroundStyle(.secondary)
                    if pod.restarts > 0 {
                        Text("restarts: \(pod.restarts)")
                            .foregroundStyle(pod.healthy ? Color.secondary : Color.orange)
                    }
                }
                .font(.caption)
                .monospacedDigit()
            }
            Spacer()
            if deleting {
                ProgressView()
            } else {
                Button(action: onDelete) {
                    Image(systemName: "trash")
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                    .buttonStyle(.borderless)
                    .disabled(pod.status == "Terminating")
                    .accessibilityLabel(Text("Delete \(pod.name)"))
            }
        }
    }

    private var statusColor: Color {
        if pod.healthy { return .green }
        return pod.transitional ? .orange : .red
    }
}
