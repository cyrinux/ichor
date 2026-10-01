import SwiftUI
import TalosViewerCore

struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var confirmDelete = false
    @State private var lockError: String?

    var body: some View {
        @Bindable var model = model
        Form {
            if let contexts = model.summary?.contexts {
                Section("Context") {
                    Picker("Active context", selection: $model.activeContext) {
                        ForEach(contexts) { Text($0.name).tag($0.name) }
                    }
                    if let ctx = model.activeSummary {
                        LabeledContent("Endpoints", value: ctx.endpoints.joined(separator: "\n"))
                        LabeledContent("Nodes", value: "\(ctx.nodes.isEmpty ? ctx.endpoints.count : ctx.nodes.count)")
                        LabeledContent("Roles", value: ctx.roles.joined(separator: ", "))
                        LabeledContent("Cert expires", value: certExpiryText(ctx.certNotAfter))
                    }
                }
            }
            Section("Appearance") {
                Picker("Theme", selection: $model.theme) {
                    ForEach(ThemeMode.allCases) { Text($0.label).tag($0) }
                }
                .pickerStyle(.segmented)
            }
            Section {
                Toggle("App lock", isOn: Binding(get: { model.lock.enabled }, set: { setLock($0) }))
                if let lockError { Text(lockError).font(.footnote).foregroundStyle(.red) }
            } header: {
                Text("Security")
            } footer: {
                Text("Face ID / Touch ID, or the device passcode, to open the app and before reboot or shutdown. Also hides the app in the app switcher.")
            }
            Section("Config") {
                NavigationLink("Import a new talosconfig", value: Route.importConfig)
                Button("Delete stored talosconfig", role: .destructive) { confirmDelete = true }
            }
            Section {
                LabeledContent("Version", value: version)
            }
        }
        .themedBackground()
        .navigationTitle("Settings")
        .confirmationDialog("Delete talosconfig?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) { model.clear() }
        } message: {
            Text("The config and its client key will be removed from this device.")
        }
    }

    /// Both enabling and disabling require authenticating first.
    private func setLock(_ enabled: Bool) {
        guard Authenticator.isAvailable else {
            lockError = "Set up a passcode (and optionally Face ID) on this device first."
            return
        }
        Task {
            if let failure = await Authenticator.authenticate(reason: enabled ? "Enable app lock" : "Disable app lock") {
                lockError = failure
            } else {
                lockError = nil
                model.setLockEnabled(enabled)
            }
        }
    }

    private var version: String {
        let info = Bundle.main.infoDictionary
        let short = info?["CFBundleShortVersionString"] as? String ?? "?"
        let build = info?["CFBundleVersion"] as? String ?? "?"
        return "\(short) (\(build))"
    }
}
