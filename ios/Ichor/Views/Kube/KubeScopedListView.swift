import SwiftUI
import IchorCore

/// A list of the Kubernetes screens' namespace scope: the scope bar, "type the namespace" until
/// one is chosen, then `content` for what `fetch` read. It reloads when the scope, the cluster,
/// its API address or the screenshot mode changes; `content` gets the scope it was read for and
/// `reload` (after an action, or on a pull).
struct KubeScopedList<Value, Content: View>: View {
    let title: Text
    let prompt: Text
    @Binding var query: String
    let fetch: @MainActor (TalosClient, KubeScope) async throws -> Value
    /// The namespaces of what was read, offered by the scope bar.
    let namespaces: (Value) -> [String]
    let content: (Value, KubeScope, _ reload: @escaping () async -> Void) -> Content

    @Environment(AppModel.self) private var model
    @State private var scope = KubeBrowserScope()
    @State private var state: LoadState<Value> = .loading

    // Explicit: the private @State makes the memberwise init private.
    init(
        title: Text,
        prompt: Text,
        query: Binding<String>,
        fetch: @escaping @MainActor (TalosClient, KubeScope) async throws -> Value,
        namespaces: @escaping (Value) -> [String],
        @ViewBuilder content: @escaping (Value, KubeScope, _ reload: @escaping () async -> Void) -> Content
    ) {
        self.title = title
        self.prompt = prompt
        _query = query
        self.fetch = fetch
        self.namespaces = namespaces
        self.content = content
    }

    /// What decides the list: the scope, and the cluster, API address and screenshot mode.
    private struct Trigger: Hashable {
        let scope: KubeScope
        let ready: Bool
        let source: String
    }

    var body: some View {
        let control = scope.control(model: model)
        VStack(spacing: 0) {
            KubeScopeBar(control: control, loaded: loadedNamespaces)
                .background(.bar)
            Divider()
            if !control.ready {
                ContentUnavailableView {
                    Label("Namespace", systemImage: "square.dashed")
                } description: {
                    Text("Type the namespace to list above.")
                }
                .frame(maxHeight: .infinity)
            } else {
                LoadStateView(state: state, retry: { await load(control.scope) }) { value in
                    content(value, control.scope) { await load(control.scope) }
                }
            }
        }
        .searchable(text: $query, prompt: prompt)
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: kubeNamespacesKey(model)) { await scope.loadNamespaces(model: model) }
        .task(id: Trigger(scope: control.scope, ready: control.ready, source: kubeNamespacesKey(model))) {
            if control.ready { await load(control.scope) }
        }
    }

    private var loadedNamespaces: [String] {
        guard case .loaded(let value, _, _) = state else { return [] }
        return Array(Set(namespaces(value))).sorted()
    }

    private func load(_ scope: KubeScope) async {
        guard let client = model.client else { return }
        let key = kubeNamespacesKey(model)
        let fetched: LoadState<Value> = await .from { try await fetch(client, scope) }
        guard key == kubeNamespacesKey(model), !Task.isCancelled else { return }
        state = state.refreshed(with: fetched)
    }
}
