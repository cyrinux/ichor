import SwiftUI
import IchorCore

/// The PersistentVolumeClaims of the Kubernetes screens' namespace scope, problems first: lost
/// and nearly full ones, then pending, terminating and filling ones, with their volume, class,
/// pods and fill (kubelet stats). A claim opens its summary; one of Longhorn, Rook Ceph or
/// CloudNativePG links to that data service.
struct KubeStorageView: View {
    @Environment(AppModel.self) private var model
    @State private var scope = KubeBrowserScope()
    @State private var state: LoadState<KubeStorage> = .loading
    @State private var query = ""

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
                LoadStateView(state: state, retry: { await load(control.scope) }) { storage in
                    list(storage, showNamespace: control.scope.namespace == nil, scope: control.scope)
                }
            }
        }
        .searchable(text: $query, prompt: Text("Search PVCs, classes or pods"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text("Storage"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: kubeNamespacesKey(model)) { await scope.loadNamespaces(model: model) }
        .task(id: Trigger(scope: control.scope, ready: control.ready, source: kubeNamespacesKey(model))) {
            if control.ready { await load(control.scope) }
        }
    }

    private func list(_ storage: KubeStorage, showNamespace: Bool, scope: KubeScope) -> some View {
        let shown = filterStorageClaims(storage.claims, query: query)
        return List {
            if storage.partialAccess {
                Text("These credentials may not read volumes, classes or nodes: some details are missing.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(shown) { claim in
                NavigationLink {
                    KubeObjectView(resource: KubeAPIResource(resource: "persistentvolumeclaims", kind: "PersistentVolumeClaim"),
                                   namespace: claim.namespace, name: claim.name)
                } label: {
                    StorageClaimRow(claim: claim, showNamespace: showNamespace)
                }
                if let kind = claim.managedKind {
                    NavigationLink(value: Route.dataServices(hints: "", downNodes: [], kind: kind)) {
                        Label(String(localized: "Open in \(kind.title)"), systemImage: "externaldrive.connected.to.line.below")
                            .font(.footnote)
                    }
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) {
            ContentUnavailableView("No PVCs.", systemImage: "externaldrive")
        }
        .refreshable { await load(scope) }
        .themedBackground()
    }

    private var loadedNamespaces: [String] {
        guard case .loaded(let storage, _, _) = state else { return [] }
        return Array(Set(storage.claims.map(\.namespace))).sorted()
    }

    private func load(_ scope: KubeScope) async {
        guard let client = model.client else { return }
        let key = kubeNamespacesKey(model)
        let fetched: LoadState<KubeStorage> = await .from { try await client.storage(namespace: scope.namespace) }
        guard key == kubeNamespacesKey(model), !Task.isCancelled else { return }
        state = state.refreshed(with: fetched)
    }
}

/// A claim: name and phase (coloured by level), fill bar or "fill not measured", class,
/// volume and access modes, the pods using it.
private struct StorageClaimRow: View {
    let claim: StorageClaim
    let showNamespace: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: showNamespace ? claim.id : claim.name)
                    .font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: claim.terminating ? "Terminating" : claim.phase)
                    .font(.caption).foregroundStyle(levelColor)
            }
            if claim.measured {
                HStack(spacing: 8) {
                    UsageBar(fraction: claim.usedFraction)
                    Text(verbatim: "\(formatBytes(Int64(claim.used))) / \(formatBytes(Int64(claim.capacity)))")
                        .font(.caption).foregroundStyle(.secondary).monospacedDigit()
                }
            } else {
                Text(verbatim: [claim.capacity > 0 ? formatBytes(Int64(claim.capacity)) : nil,
                                String(localized: "fill not measured")].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary)
            }
            let details = [claim.storageClass, claim.volume, claim.accessModes.joined(separator: ",")].filter { !$0.isEmpty }
            if !details.isEmpty {
                Text(verbatim: details.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            if !claim.pods.isEmpty {
                Text("Used by \(claim.pods.joined(separator: ", "))").font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var levelColor: Color {
        switch claim.level {
        case .critical: .statusBad
        case .warning: .statusWarn
        case .ok: .secondary
        }
    }
}
