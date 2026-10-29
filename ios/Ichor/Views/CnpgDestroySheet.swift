import SwiftUI
import IchorCore

struct CnpgRemovalTarget: Identifiable {
    let cluster: CnpgCluster
    let instance: String
    var id: String { cluster.id + "/" + instance }
}

/// Confirms removing one instance (its volumes, then its pod) after a fresh preview: where its
/// volumes live and what blocks the removal. The instance name is typed to confirm.
struct CnpgDestroySheet: View {
    let cluster: CnpgCluster
    let instance: String
    let onConfirm: () -> Void
    @Environment(AppModel.self) private var model
    @Environment(\.kubeActionAccess) private var access
    @State private var preview: CnpgDestroyPreview?
    @State private var error: String?

    private var denial: KubeAccess? { access?.denial(for: .cnpgDestroy, in: cluster.namespace) }
    private var message: String {
        var lines = [
            String(
                localized:
                    "Deletes the instance's volumes, then its pod, like kubectl cnpg destroy. CloudNativePG then clones a new replica from the primary onto a node that exists."
            )
        ]
        if let p = preview {
            lines += previewLines(p)
        } else if error == nil {
            lines.append(String(localized: "Reading removal preview…"))
        }
        if let error { lines.append(error) }
        if let denial { lines.append(denial.deniedText) }
        return lines.joined(separator: "\n\n")
    }

    private func previewLines(_ p: CnpgDestroyPreview) -> [String] {
        var lines: [String] = []
        if !p.currentPrimary.isEmpty { lines.append(String(localized: "Current primary: \(p.currentPrimary)")) }
        let pod = [p.podPhase, p.podNode].filter { !$0.isEmpty }.joined(separator: " · ")
        lines.append(pod.isEmpty ? String(localized: "No pod") : String(localized: "Pod: \(pod)"))
        lines += p.volumes.map { "\($0.name) · \(volumeWhere($0))" }
        if p.blockers.contains("primary") {
            lines.append(String(localized: "The primary cannot be removed, nor any instance while no primary is known."))
        }
        if p.blockers.contains("noOtherData") {
            lines.append(
                String(localized: "The primary keeps no volume: this instance may hold the only copy of the data."))
        }
        if p.blockers.contains("notFound") {
            lines.append(String(localized: "This instance has no pod and no volume left."))
        }
        if p.warnings.contains("ready") {
            lines.append(
                String(
                    localized:
                        "This replica is healthy: the cluster has one copy less until the new replica catches up."))
        }
        if p.warnings.contains("nodePresent") {
            lines.append(
                String(
                    localized:
                        "Its node is still there: the instance may come back on its own once the node is fixed."))
        }
        return lines
    }

    private func volumeWhere(_ v: CnpgDestroyVolume) -> String {
        switch v.nodeState {
        case "gone": String(localized: "on node \(v.node), which no longer exists")
        case "present": String(localized: "on node \(v.node), still in the cluster")
        case "none": String(localized: "not tied to a node")
        default: String(localized: "node unknown")
        }
    }

    var body: some View {
        HostnameConfirmationSheet(
            title: String(localized: "Remove instance"), message: message, hostname: instance,
            actionTitle: String(localized: "Remove instance"), enabled: preview?.canDestroy == true && denial == nil,
            onConfirm: onConfirm
        )
        .task(id: kubeNamespacesKey(model)) {
            preview = nil
            error = nil
            guard let client = model.client else { return }
            do {
                preview = try await client.cnpgDestroyPreview(
                    namespace: cluster.namespace, name: cluster.name, instance: instance)
            } catch { if !Task.isCancelled { self.error = error.localizedDescription } }
        }
    }
}
