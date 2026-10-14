import SwiftUI
import IchorCore

struct CnpgPromotionTarget: Identifiable {
    let cluster: CnpgCluster
    let instance: String
    var id: String { cluster.id + "/" + instance }
}

struct CnpgPromoteSheet: View {
    let cluster: CnpgCluster
    let instance: String
    let onConfirm: () -> Void
    @Environment(AppModel.self) private var model
    @Environment(\.kubeActionAccess) private var access
    @State private var preview: CnpgPromotePreview?
    @State private var error: String?

    private var denial: KubeAccess? { access?.denial(for: .cnpgPromote, in: cluster.namespace) }
    private var message: String {
        var lines = [String(localized: "Promotion switches the primary and interrupts connections. Data that has not reached the replica may be lost.")]
        if let p = preview {
            lines.append(String(localized: "Current primary: \(p.currentPrimary)"))
            lines.append(String(localized: "Target instance: \(p.instance)"))
            let lag = p.lagBytes.map { formatBytes($0) } ?? String(localized: "Unknown replication lag")
            lines.append(String(localized: "Replication lag: \(lag)"))
            if p.warnings.contains("lagNonzero") { lines.append(String(localized: "The replica is behind the primary. Promotion may lose data.")) }
            if p.warnings.contains("lagUnknown") { lines.append(String(localized: "Replication lag could not be verified. Promotion may lose data.")) }
            if !p.canPromote { lines.append(String(localized: "This instance cannot be promoted in the cluster's current state.")) }
            if !p.lagError.isEmpty { lines.append(p.lagError) }
        } else if error == nil { lines.append(String(localized: "Reading promotion preview…")) }
        if let error { lines.append(error) }
        if let denial { lines.append(denial.deniedText) }
        return lines.joined(separator: "\n\n")
    }
    var body: some View {
        HostnameConfirmationSheet(title: String(localized: "Promote replica"), message: message, hostname: cluster.name,
                                  actionTitle: String(localized: "Promote replica"), enabled: preview?.canPromote == true && denial == nil,
                                  onConfirm: onConfirm)
        .task(id: kubeNamespacesKey(model)) {
            preview = nil
            error = nil
            guard let client = model.client else { return }
            do { preview = try await client.cnpgPromotePreview(namespace: cluster.namespace, name: cluster.name, instance: instance) }
            catch { if !Task.isCancelled { self.error = error.localizedDescription } }
        }
    }
}
