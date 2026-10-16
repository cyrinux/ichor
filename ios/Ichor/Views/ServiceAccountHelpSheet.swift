import SwiftUI
import Ichorgo
import IchorCore

/// "How to create a service account" as its own sheet (import preview, cluster home, cluster list).
struct ServiceAccountHelpSheet: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ServiceAccountHelpView()
                .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }
}

/// A dedicated ServiceAccount with a long-lived token Secret and least-privilege RBAC, then a
/// kubeconfig for it: the templates come from the Go core, the same as on Android.
struct ServiceAccountHelpView: View {
    @State private var templates: [(role: ServiceAccountRole, template: ServiceAccountTemplate)] = []
    @State private var role = ServiceAccountRole.read

    var body: some View {
        List {
            Section {
                Text("Give the phone its own ServiceAccount rather than your admin credentials: if the phone is lost, its token only grants what you chose, and deleting the ServiceAccount revokes it.")
                    .font(.callout)
                Text("Never bind it to cluster-admin: whoever holds the phone could then change or delete anything in the cluster.")
                    .font(.footnote).foregroundStyle(.statusWarn)
                Text("A token from kubectl create token (TokenRequest) expires, after an hour by default. The kubernetes.io/service-account-token Secret below holds one that lasts until you delete it, for long-lived access from the phone.")
                    .font(.footnote).foregroundStyle(.secondary)
                Picker("Role", selection: $role) {
                    ForEach(templates, id: \.role) { Text(Self.label($0.role)).tag($0.role) }
                }
                .pickerStyle(.segmented)
                Text(Self.grants(role)).font(.footnote).foregroundStyle(.secondary)
            }
            if let template = templates.first(where: { $0.role == role })?.template {
                Section {
                    CommandRow(template.yaml, copyLabel: "Copy YAML")
                    CommandRow(template.apply)
                } header: {
                    Text("1. Save it as ichor-phone.yaml (for another namespace, replace ichor everywhere), then apply it:")
                        .textCase(nil)
                }
                Section {
                    CommandRow(template.kubeconfig)
                } header: {
                    Text("2. Build kubeconfig-phone from its token, with the server of your current context:")
                        .textCase(nil)
                } footer: {
                    Text("3. Bring kubeconfig-phone to the phone and import it, then delete the other copies: the token is a password.")
                }
            }
        }
        .navigationTitle("How to create a service account")
        .navigationBarTitleDisplayMode(.inline)
        .task { templates = knownServiceAccountTemplates((try? await TalosClient.serviceAccountTemplates()) ?? []) }
    }

    private static func label(_ role: ServiceAccountRole) -> String {
        switch role {
        case .read: String(localized: "Read-only")
        case .operate: String(localized: "Read + restart")
        case .namespace: String(localized: "One namespace")
        }
    }

    private static func grants(_ role: ServiceAccountRole) -> String {
        switch role {
        case .read:
            String(localized: "The built-in view role in every namespace, plus nodes: workloads, events, logs and metrics, no Secrets. Recommended for a phone.")
        case .operate:
            String(localized: "Read-only, plus restarting Deployments, StatefulSets and DaemonSets and scaling Deployments and StatefulSets.")
        case .namespace:
            String(localized: "Everything inside the ichor namespace (the built-in admin role there), nothing outside it: no nodes, no other namespace.")
        }
    }
}

extension TalosClient {
    /// The ServiceAccount help sheet's templates (no network).
    static func serviceAccountTemplates() async throws -> [ServiceAccountTemplate] {
        try await json { IchorgoServiceAccountTemplates($0) }
    }
}
