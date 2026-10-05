import SwiftUI
import IchorCore

struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @State private var confirmDelete = false
    @State private var lockError: String?

    var body: some View {
        @Bindable var model = model
        Form {
            Section {
                Picker("Theme", selection: $model.theme) {
                    ForEach(ThemeMode.allCases) { Text($0.label).tag($0) }
                }
                .pickerStyle(.segmented)
                // iOS manages the per-app language in the Settings app (the bundle declares its localizations).
                Button {
                    if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                } label: {
                    LabeledContent("Language", value: Self.currentLanguage)
                }
                .foregroundStyle(.primary)
            } header: {
                Text("Appearance")
            } footer: {
                Text("Ichor follows the iPhone language. To use another one for this app only, open its page in the Settings app and choose Language.")
            }
            LiveStatsSection()
            Section {
                Toggle("App lock", isOn: Binding(get: { model.lock.enabled }, set: { setLock($0) }))
                    .disabled(lockRequired && model.lock.enabled)
                if let lockError { Text(lockError).font(.footnote).foregroundStyle(.statusBad) }
            } header: {
                Text("Security")
            } footer: {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Face ID / Touch ID, or the device passcode, to open the app and before reboot or shutdown. Also hides the app in the app switcher.")
                    if lockRequired && model.lock.enabled { Text("Always on while a cluster is imported.") }
                }
            }
            PrivacySection()
            AppIconsSection()
            MonitoringSection()
            AISection()
            if model.allows(.kubeconfig) { KubeconfigSection() }
            Section("Config") {
                if let protection = SecureConfigStore.protection {
                    LabeledContent("Encryption key", value: protection.label)
                }
                NavigationLink("Add a cluster (import a talosconfig)", value: Route.importConfig)
                if model.allows(.issueConfig) {
                    NavigationLink("Renew my certificate…", value: Route.issueConfig(renew: true))
                    NavigationLink("Create a config for another device…", value: Route.issueConfig(renew: false))
                }
                Button("Delete stored talosconfig", role: .destructive) { confirmDelete = true }
            }
            BackupSection()
            if model.allows(.supportBundle) {
                let support = model.clusterSupport(.supportBundle)
                Section {
                    NavigationLink(value: Route.supportBundle) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Create a support bundle…")
                            if let notice = support.localizedNotice {
                                Text(notice).font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                    .disabled(!support.supported)
                } header: {
                    Text("Troubleshooting")
                } footer: {
                    Text("Collect logs and cluster details of the nodes into a zip, like talosctl support.")
                }
            }
            AboutSection()
        }
        .themedBackground()
        .navigationTitle("Settings")
        .confirmationDialog("Delete talosconfig?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) { model.clear() }
        } message: {
            Text("The config of every cluster, with their client keys, will be removed from this device.")
        }
    }

    /// The language the app is shown in, named in that language ("Français", "Deutsch"…).
    private static var currentLanguage: String {
        let code = Bundle.main.preferredLocalizations.first ?? "en"
        let name = Locale(identifier: code).localizedString(forLanguageCode: code) ?? code
        return name.prefix(1).uppercased() + name.dropFirst()
    }

    /// The lock cannot be turned off while a real cluster is stored.
    private var lockRequired: Bool { AppLockState.required(for: model.summary?.contexts ?? []) }

    /// Both enabling and disabling require authenticating first.
    private func setLock(_ enabled: Bool) {
        guard Authenticator.isAvailable else {
            lockError = String(localized: "Set up a passcode (and optionally Face ID) on this device first.")
            return
        }
        Task {
            if let failure = await Authenticator.authenticate(reason: enabled ? String(localized: "Enable app lock") : String(localized: "Disable app lock")) {
                lockError = failure
            } else {
                lockError = nil
                model.setLockEnabled(enabled)
            }
        }
    }
}

/// Icons of the Apps inventory: off, only the bundled ones are shown (a third-party request).
private struct AppIconsSection: View {
    @AppStorage(AppIconSettings.remoteKey) private var remoteIcons = false

    var body: some View {
        Section {
            Toggle("Download missing app icons", isOn: $remoteIcons)
        } header: {
            Text("Apps")
        } footer: {
            Text("About 250 common apps have bundled icons. When on, icons for other recognised apps are downloaded from jsDelivr (Dashboard Icons). Only the public icon name is sent, never your image names or cluster details. Icons an Argo CD app links in its ichor.levis.name/icon annotation are downloaded too.")
        }
    }
}

/// Screenshot mode: Go masks IPs, node and context names, plus the extra words. The words
/// apply when the field is submitted or the screen closes, not on every keystroke. And the
/// last known state, kept on the phone only when turned on.
private struct PrivacySection: View {
    @Environment(AppModel.self) private var model
    @State private var words = ""
    @FocusState private var editingWords: Bool

    var body: some View {
        Section {
            Toggle("Screenshot mode", isOn: Binding(get: { model.privacyMask }, set: { on in
                Task { await model.setPrivacyMask(on, words: normalizedMaskWords(words)) }
            }))
            if model.privacyMask {
                VStack(alignment: .leading, spacing: 4) {
                    TextField("Also hide these words", text: $words)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($editingWords)
                        .onSubmit(commit)
                    Text("Comma-separated, e.g. a domain or a customer name").font(.caption).foregroundStyle(.secondary)
                }
            }
            // Off: what was kept is deleted (AppModel.setKeepLastKnown).
            VStack(alignment: .leading, spacing: 4) {
                Toggle("Keep last known state", isOn: Binding(get: { model.keepLastKnown }, set: { model.setKeepLastKnown($0) }))
                Text("Save the last data fetched from each cluster on this phone, encrypted, so it still shows when the cluster can't be reached. Kept for 24 hours and never backed up.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Privacy")
        } footer: {
            Text("Hide IP addresses, node names and app logos, e.g. for screenshots or screen sharing")
        }
        .onAppear { words = model.privacyWords }
        .onChange(of: editingWords) { _, editing in if !editing { commit() } }
        .onDisappear(perform: commit)
    }

    private func commit() {
        words = normalizedMaskWords(words)
        guard model.privacyMask, words != model.privacyWords else { return }
        Task { await model.setPrivacyMask(true, words: words) }
    }
}
