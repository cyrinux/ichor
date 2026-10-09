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
    /// A share link's pod: its logs open once listed.
    @Binding var focus: String?

    @Environment(AppModel.self) private var model
    @State private var actions = PodActions()
    /// Pod usage from metrics-server, by pod id; empty without it (no bars nor sort then).
    @State private var top: [String: KubeTopPod] = [:]
    @State private var topRefreshes = 0
    @State private var sort = TopSort.name

    var body: some View {
        KubeListFrame(control: control, list: list, query: query, namespaces: podNamespaces) { load in
            let selected = control.scope.namespace
            // Sorted once complete; image search only when every row carries its images.
            let filtered = filterPods(load.items, namespace: selected, query: query, sorted: load.done, searchImages: load.detailed)
            let shown = filtered.sortedByUsage(sort) { top[$0.id] }
            List {
                KubeDeniedSection(actions: [.deletePod], namespace: selected ?? "")
                if !top.isEmpty {
                    TopSortPicker(sort: $sort).listRowSeparator(.hidden)
                }
                Section {
                    ForEach(shown) { pod in
                        PodRow(pod: pod, showNamespace: selected == nil, usage: top[pod.id], deleting: actions.deleting.contains(pod.id),
                               onLogs: { actions.logsPod = pod }) { actions.confirm = pod }
                            .podLogsSwipe { actions.logsPod = pod }
                            .contextMenu {
                                Button { actions.logsPod = pod } label: { Label("Logs", systemImage: "doc.text") }
                                ShareLinkButton(target: .pod(namespace: pod.namespace, name: pod.name))
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
            .emptyOverlay(shown.isEmpty, query: query) { ContentUnavailableView("No pods", systemImage: "cube") }
            .refreshable {
                await list.refresh(model: model)
                topRefreshes += 1
            }
            .themedBackground()
            .task(id: "\(selected ?? "")#\(topRefreshes)") {
                let usage = try? await model.client?.topPods(namespace: selected)
                top = usage?.available == true ? usage?.byKey ?? [:] : [:]
            }
        }
        .podActions(actions) { await list.refresh(model: model) }
        .task(id: list.loadedItems.map(\.id)) {
            guard let focus, let pod = list.loadedItems.first(where: { $0.id == focus }) else { return }
            actions.logsPod = pod
            self.focus = nil
        }
    }
}

/// What a pod list's rows asked for (os:admin): the logs to show, and a deletion, confirmed
/// first, so a controller starts a fresh pod.
@Observable
@MainActor
final class PodActions {
    var confirm: KubePod?
    var logsPod: KubePod?
    /// Ids of the pods whose deletion is in flight.
    var deleting: Set<String> = []
    var resultMessage: String?

    func delete(_ pod: KubePod, model: AppModel, reload: () async -> Void) async {
        guard let client = model.client, !deleting.contains(pod.id) else { return }
        deleting.insert(pod.id)
        defer { deleting.remove(pod.id) }
        do {
            try await client.deletePod(pod)
            resultMessage = String(localized: "\(pod.name) is being deleted")
            // Shows it terminating, and soon its replacement.
            await reload()
        } catch {
            resultMessage = String(localized: "Could not delete \(pod.name): \(error.localizedDescription)")
        }
    }
}

extension View {
    /// Swipe right on a pod's row: its logs, as its Logs button and its long press.
    func podLogsSwipe(_ onLogs: @escaping () -> Void) -> some View {
        swipeActions(edge: .leading) {
            Button(action: onLogs) { Label("Logs", systemImage: "doc.text") }
                .tint(.blue)
        }
    }

    /// The delete confirmation, its outcome and the logs sheet of `actions`; `reload` loads the
    /// list again after a deletion.
    func podActions(_ actions: PodActions, reload: @escaping () async -> Void) -> some View {
        modifier(PodActionsModifier(actions: actions, reload: reload))
    }
}

private struct PodActionsModifier: ViewModifier {
    @Bindable var actions: PodActions
    let reload: () async -> Void

    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        content
            .confirmationDialog(actions.confirm.map { String(localized: "Delete pod \($0.name)?") } ?? "",
                                isPresented: $actions.confirm.isPresent(),
                                titleVisibility: .visible,
                                presenting: actions.confirm) { pod in
                Button("Delete", role: .destructive) {
                    Task { await actions.delete(pod, model: model, reload: reload) }
                }
                Button("Cancel", role: .cancel) {}
            } message: { pod in
                if pod.owner.isEmpty {
                    Text("It is removed from \(pod.namespace) after its grace period. No controller owns it: it will not come back.")
                } else {
                    Text("It is removed from \(pod.namespace) after its grace period; \(pod.owner) starts a new one.")
                }
            }
            .messageAlert($actions.resultMessage)
            .sheet(item: $actions.logsPod) { PodLogsSheet(pod: $0) }
    }
}

/// A pod: name, namespace and node (when asked), status, readiness, restarts; logs and delete.
struct PodRow: View {
    let pod: KubePod
    let showNamespace: Bool
    var showNode = true
    /// CPU and memory in use, from metrics-server when there is one.
    var usage: KubeTopPod?
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
                Text(verbatim: [showNamespace ? pod.namespace : nil, showNode && !pod.node.isEmpty ? pod.node : nil]
                    .compactMap { $0 }.joined(separator: " · "))
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
                if let usage { PodUsageView(top: usage).padding(.trailing, 8) }
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
                    .kubeGated(.deletePod, in: pod.namespace)
                    .accessibilityLabel(Text("Delete \(pod.name)"))
            }
        }
    }

    private var statusColor: Color {
        if pod.healthy { return .green }
        return pod.transitional ? .orange : .red
    }
}
