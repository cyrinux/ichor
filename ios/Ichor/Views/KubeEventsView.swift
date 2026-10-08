import SwiftUI
import IchorCore

/// The events of every namespace and of the nodes, newest first, like `kubectl get events -A`:
/// the Kubernetes counterpart of the Talos events, for a cluster added from a kubeconfig. The
/// Warning ones by default; Kubernetes keeps events for an hour. Loaded on demand, never polled.
struct KubeEventsView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeEventList> = .loading
    /// The Normal events are left out.
    @State private var warningsOnly = true

    var body: some View {
        VStack(spacing: 0) {
            Picker("Events", selection: $warningsOnly) {
                Text("Warnings only").tag(true)
                Text("All").tag(false)
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.vertical, 8)
            Divider()
            LoadStateView(state: state, retry: load) { list in
                List {
                    if list.forbidden {
                        Text("These credentials cannot list the events of the whole cluster.")
                            .font(.callout).foregroundStyle(.secondary)
                    } else if list.events.isEmpty {
                        Text(verbatim: CheckupText.kubeEventsEmpty).font(.callout).foregroundStyle(.secondary)
                    } else {
                        let now = Int64(Date().timeIntervalSince1970 * 1000)
                        // In the server's order (newest first); an index id, as the same event can repeat.
                        ForEach(Array(list.events.enumerated()), id: \.offset) { _, event in
                            KubeEventRow(event: event, now: now, showObject: true, showNamespace: true)
                        }
                    }
                }
                .refreshable { await load() }
                .themedBackground()
            }
        }
        // Another filter, cluster or screenshot mode: the list again, from scratch.
        .task(id: "\(warningsOnly)#\(model.activeContext)#\(model.dataGeneration)") {
            state = .loading
            await load()
        }
        .navigationTitle(Text(verbatim: CheckupText.kubeEventsTitle))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func load() async {
        guard let client = model.client else { return }
        let warnings = warningsOnly
        state = state.refreshed(with: await .from { try await client.kubeClusterEvents(warningsOnly: warnings) })
    }
}
