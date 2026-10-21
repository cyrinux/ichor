import SwiftUI
import IchorCore

/// The PersistentVolumeClaims of the Kubernetes screens' namespace scope, problems first: lost
/// and nearly full ones, then pending, terminating and filling ones, with their volume, class,
/// pods and fill (kubelet stats). A claim opens its summary; one of Longhorn or CloudNativePG
/// links to that data service, one of Rook Ceph to its plugin.
struct KubeStorageView: View {
    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var snapshots: VolumeSnapshots?
    @State private var confirmSnapshot: StorageClaim?
    @State private var snapshotBusy = false
    @State private var snapshotResult: String?

    var body: some View {
        KubeScopedList(
            title: Text("Storage"),
            prompt: Text("Search PVCs, classes or pods"),
            query: $query,
            fetch: { (client: TalosClient, scope: KubeScope) async throws -> KubeStorage in
                let key = kubeNamespacesKey(model)
                snapshots = nil
                confirmSnapshot = nil
                let snapshotData = try? await client.volumeSnapshots(namespace: scope.namespace ?? "")
                defer { if key == kubeNamespacesKey(model), !Task.isCancelled { snapshots = snapshotData } }
                return try await client.storage(namespace: scope.namespace)
            },
            namespaces: { value in value.claims.map(\.namespace) }
        ) { storage, scope, reload in
            list(storage, showNamespace: scope.namespace == nil, scope: scope, reload: reload)
        }
    }

    private func list(_ storage: KubeStorage, showNamespace: Bool, scope: KubeScope, reload: @escaping () async -> Void) -> some View {
        let shown = filterStorageClaims(storage.claims, query: query)
        return List {
            if snapshotBusy { ProgressView() }
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
                } else if let plugin = claim.managedPlugin {
                    NavigationLink(value: Route.plugin(id: plugin)) {
                        Label(String(localized: "Open in \(claim.managedName)"), systemImage: "externaldrive.connected.to.line.below")
                            .font(.footnote)
                    }
                }
                if model.activeSummary?.demo == false, snapshots?.canCreate(claim) == true {
                    VStack(alignment: .leading) {
                        Button("Create snapshot") { confirmSnapshot = claim }.disabled(snapshotBusy).kubeGated(.createVolumeSnapshot, in: claim.namespace)
                        KubeDeniedNote(.createVolumeSnapshot, in: claim.namespace)
                    }.loadsKubeActionAccess(namespace: claim.namespace)
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) {
            ContentUnavailableView("No PVCs.", systemImage: "externaldrive")
        }
        .refreshable { await reload() }
        .themedBackground()
        .loadsKubeActionAccess(namespace: scope.namespace)
        .confirmationDialog("Create snapshot", isPresented: Binding(get: { confirmSnapshot != nil }, set: { if !$0 { confirmSnapshot = nil } })) {
            Button("Create snapshot") {
                guard let claim = confirmSnapshot, let client = model.client else { return }
                confirmSnapshot = nil
                snapshotBusy = true
                let source = kubeNamespacesKey(model)
                Task {
                    do { snapshotResult = try await client.createVolumeSnapshot(namespace: claim.namespace, pvc: claim.name) }
                    catch { snapshotResult = error.localizedDescription }
                    snapshotBusy = false
                    if source == kubeNamespacesKey(model) { await reload() }
                }
            }
        } message: {
            Text("Create a CSI snapshot of \(confirmSnapshot?.id ?? "") using the default class for its driver.")
        }
        .alert("Create snapshot", isPresented: Binding(get: { snapshotResult != nil }, set: { if !$0 { snapshotResult = nil } })) {
            Button("OK") { snapshotResult = nil }
        } message: { Text(verbatim: snapshotResult ?? "") }
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
