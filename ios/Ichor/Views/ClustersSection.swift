import SwiftUI
import IchorCore

/// Settings → Clusters & backup: the encryption key, adding a cluster, renewing the certificate
/// or issuing a config for another device (os:admin), back up / restore, and deleting every cluster.
struct ClustersSection: View {
    @Environment(AppModel.self) private var model
    @State private var confirmDelete = false
    /// "Back up first" was chosen: the backup starts once the delete sheet is gone.
    @State private var backupAfterSheet = false
    @State private var backupRequested = false

    var body: some View {
        Section {
            if let protection = SecureConfigStore.protection {
                LabeledContent("Encryption key", value: protection.label)
            }
            NavigationLink(model.kubeYAML == nil ? LocalizedStringKey("Add a cluster (import a talosconfig)") : "Add a cluster", value: Route.importConfig)
            if model.allows(.issueConfig) {
                NavigationLink("Renew my certificate…", value: Route.issueConfig(renew: true))
                NavigationLink("Create a config for another device…", value: Route.issueConfig(renew: false))
            }
            // Both stores go: with kubeconfig clusters stored, say so.
            Button(model.kubeYAML == nil ? LocalizedStringKey("Delete stored talosconfig") : "Delete all clusters", role: .destructive) {
                confirmDelete = true
            }
        } header: {
            Text("Config")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                Text("Add a cluster imports a talosconfig or a kubeconfig. The clusters already stored are kept.")
                if model.allows(.issueConfig) {
                    Text("Renew this context's client certificate, or issue a restricted talosconfig for another device.")
                }
                // README "Leftover data": the Keychain outlives the app.
                Text("Configs are stored in the iOS Keychain, which keeps them after the app is uninstalled. Delete them here to remove them from this device.")
            }
        }
        // Every cluster's client keys go, for good without a backup: typed, like a reboot.
        .sheet(isPresented: $confirmDelete, onDismiss: {
            if backupAfterSheet {
                backupAfterSheet = false
                backupRequested = true
            }
        }) {
            HostnameConfirmationSheet(
                title: model.kubeYAML == nil ? String(localized: "Delete talosconfig?") : String(localized: "Delete all clusters?"),
                message: String(localized: "The config of every cluster, with their client keys, will be removed from this device."),
                hostname: Self.deleteAllToken,
                actionTitle: String(localized: "Delete"),
                alternative: model.hasConfig ? (title: String(localized: "Back up first"), action: { backupAfterSheet = true }) : nil
            ) {
                confirmDelete = false
                model.clear()
            }
        }
        BackupSection(requested: $backupRequested)
    }

    /// What to type to delete every cluster; not translated, like a hostname.
    private static let deleteAllToken = "DELETE"
}
