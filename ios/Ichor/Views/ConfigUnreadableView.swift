import SwiftUI
import IchorCore

/// A stored config is there but could not be read (decrypted or parsed), as Android's
/// ConfigUnreadableScreen: nothing else is shown, so no import, removal or restore can
/// overwrite it. Try again, or delete every stored cluster and start over.
struct ConfigUnreadableView: View {
    @Environment(AppModel.self) private var model
    @State private var retrying = false
    @State private var confirmDelete = false

    var body: some View {
        ContentUnavailableView {
            Label("The stored config could not be read", systemImage: "lock.trianglebadge.exclamationmark")
        } description: {
            Text(reason)
        } actions: {
            Button("Try again") {
                retrying = true
                Task {
                    await model.load()
                    retrying = false
                }
            }
            .buttonStyle(.borderedProminent)
            .disabled(retrying)
            Button("Delete all clusters", role: .destructive) { confirmDelete = true }
        }
        .confirmationDialog("Delete all clusters?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) { model.clear() }
        } message: {
            Text("The config of every cluster, with their client keys, will be removed from this device.")
        }
    }

    /// Which store failed, named as the file the user imported.
    private var reason: String {
        let failed = Set(model.unreadable)
        if failed == [StoredConfigKind.kubeconfig] {
            return String(localized: "Your kubeconfig is still on this device, but it could not be read just now. Try again; if it keeps failing, delete the stored clusters and import them again.")
        }
        if failed == [StoredConfigKind.talosconfig] {
            return String(localized: "Your talosconfig is still on this device, but it could not be read just now. Try again; if it keeps failing, delete the stored clusters and import them again.")
        }
        return String(localized: "Your talosconfig and kubeconfig are still on this device, but they could not be read just now. Try again; if it keeps failing, delete the stored clusters and import them again.")
    }
}
