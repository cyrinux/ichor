import SwiftUI
import IchorCore

enum CnpgLifecycleAction: String {
    case restart, hibernate, wake
    var permission: KubeAction { self == .restart ? .cnpgRestartPrimary : .cnpgHibernate }
    var title: String {
        switch self {
        case .restart: String(localized: "Restart primary")
        case .hibernate: String(localized: "Hibernate cluster")
        case .wake: String(localized: "Wake cluster")
        }
    }
    var risk: String {
        switch self {
        case .restart: String(localized: "Restarting the primary interrupts connections. CloudNativePG restarts PostgreSQL in place and preserves its PVCs.")
        case .hibernate: String(localized: "Hibernation stops the cluster and interrupts all connections. Its PVCs are preserved until you wake the cluster.")
        case .wake: String(localized: "CloudNativePG starts the cluster using its preserved PVCs. Connections remain unavailable until the instances are ready.")
        }
    }
}
struct CnpgLifecycleTarget: Identifiable {
    let cluster: CnpgCluster
    let action: CnpgLifecycleAction
    var id: String { cluster.id + "|" + action.rawValue }
}
struct CnpgLifecycleSheet: View {
    let target: CnpgLifecycleTarget
    let onConfirm: () -> Void
    @Environment(\.kubeActionAccess) private var access
    var body: some View {
        let denial = access?.denial(for: target.action.permission, in: target.cluster.namespace)
        let primary = target.action == .restart ? String(localized: "Primary to restart: \(target.cluster.currentPrimary)") : ""
        HostnameConfirmationSheet(title: target.action.title,
            message: [target.action.risk, primary, denial?.deniedText ?? ""].filter { !$0.isEmpty }.joined(separator: "\n\n"),
            hostname: target.cluster.name, actionTitle: target.action.title, enabled: denial == nil, onConfirm: onConfirm)
    }
}
struct CnpgLifecycleButtons: View {
    let cluster: CnpgCluster
    let busy: Bool
    let onAction: (CnpgLifecycleAction) -> Void
    var body: some View {
        if cluster.hibernated {
            Button { onAction(.wake) } label: { Text("Wake cluster") }
                .disabled(busy).kubeGated(.cnpgHibernate, in: cluster.namespace)
        } else {
            if !cluster.currentPrimary.isEmpty {
                Button { onAction(.restart) } label: { Text("Restart primary") }
                    .disabled(busy).kubeGated(.cnpgRestartPrimary, in: cluster.namespace)
                KubeDeniedNote(.cnpgRestartPrimary, in: cluster.namespace)
            }
            Button { onAction(.hibernate) } label: { Text("Hibernate cluster") }
                .disabled(busy).kubeGated(.cnpgHibernate, in: cluster.namespace)
        }
        KubeDeniedNote(.cnpgHibernate, in: cluster.namespace)
    }
}
