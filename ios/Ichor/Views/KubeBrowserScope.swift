import SwiftUI
import IchorCore

/// The namespace the browser's lists show, shared with the Kubernetes screen's: the same
/// remembered scope per cluster (KubeScopeStore), the same default, the same picker. Not kept
/// in the demo nor in screenshot mode, where the scope lasts while the screen does.
@Observable
@MainActor
final class KubeBrowserScope {
    /// The cluster's namespaces; nil while unknown (loading, or failed).
    private(set) var namespaces: KubeNamespaces?
    private var localScope: KubeScope?
    /// Bumped when a scope is picked: the stored one is read again.
    private var edits = 0

    /// The scope control of the lists for the active cluster.
    func control(model: AppModel) -> KubeScopeControl {
        let cluster = Self.cluster(model)
        let scope = defaultScope(remembered: remembered(cluster), namespaces: namespaces)
        return KubeScopeControl(scope: scope ?? KubeScope(), namespaces: namespaces, ready: scope != nil) { [weak self] picked in
            guard let self else { return }
            if let cluster { KubeScopeStore.set(picked, for: cluster) } else { localScope = picked }
            edits += 1
        }
    }

    /// Reads the cluster's namespaces; forbidden is an answer, not a failure.
    func loadNamespaces(model: AppModel) async {
        guard let client = model.client else { return }
        namespaces = nil
        let listed = try? await client.namespaces()
        guard !Task.isCancelled else { return }
        namespaces = listed
    }

    private func remembered(_ cluster: String?) -> KubeScope? {
        _ = edits
        guard let cluster else { return localScope }
        return KubeScopeStore.scope(for: cluster)
    }

    /// The cluster whose scope is kept on the device: not the demo, not in screenshot mode.
    private static func cluster(_ model: AppModel) -> String? {
        model.activeSummary.flatMap { $0.demo || $0.fingerprint.isEmpty || model.privacyMask ? nil : $0.fingerprint }
    }
}

/// What reloads the namespaces: the cluster, its API address and the screenshot mode.
@MainActor
func kubeNamespacesKey(_ model: AppModel) -> String {
    "\(model.activeContext)|\(model.client?.kubeServer ?? "")|\(model.dataGeneration)"
}

extension KubeTone {
    /// The status colours readable as text; nil when the tone says nothing.
    var color: Color? {
        switch self {
        case .good: .statusOK
        case .warn: .statusWarn
        case .bad: .statusBad
        case .neutral: nil
        }
    }
}
