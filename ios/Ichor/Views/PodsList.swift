import SwiftUI
import IchorCore

/// The pods of the scope's namespace (every namespace by default) with the status `kubectl get
/// pods` shows, unhealthy ones first once every page is loaded, their logs through the
/// Kubernetes API (the previous run's too), and a delete action so a controller starts a fresh
/// one (os:admin).
struct PodsList: View {
    let list: PagedList<KubePod>
    let control: KubeScopeControl
    let query: String
    /// Opens the pod's live flows; nil without Cilium.
    var onFlows: ((KubePod) -> Void)?

    @Environment(AppModel.self) private var model
    @State private var confirm: KubePod?
    @State private var deleting: Set<String> = []
    @State private var resultMessage: String?
    @State private var logsPod: KubePod?

    var body: some View {
        KubeListFrame(control: control, list: list, query: query, namespaces: podNamespaces) { load in
            let selected = control.scope.namespace
            // Sorted once complete; image search only when every row carries its images.
            let shown = filterPods(load.items, namespace: selected, query: query, sorted: load.done, searchImages: load.detailed)
            List {
                Section {
                    ForEach(shown) { pod in
                        PodRow(pod: pod, showNamespace: selected == nil, deleting: deleting.contains(pod.id),
                               onLogs: { logsPod = pod }) { confirm = pod }
                            .contextMenu {
                                Button { logsPod = pod } label: { Label("Logs", systemImage: "doc.text") }
                                if let onFlows {
                                    Button { onFlows(pod) } label: {
                                        Label("Live flows of this pod", systemImage: "point.3.filled.connected.trianglepath.dotted")
                                    }
                                }
                            }
                    }
                    if load.hasMore && query.isEmpty { LoadMoreRow { list.loadMore(model: model) } }
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
        .sheet(item: $logsPod) { PodLogsSheet(pod: $0) }
    }

    private func load() async {
        await list.refresh(model: model)
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
    let onLogs: () -> Void
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
            Button(action: onLogs) {
                Image(systemName: "doc.text")
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
                .buttonStyle(.borderless)
                .accessibilityLabel(Text("Logs of \(pod.name)"))
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
