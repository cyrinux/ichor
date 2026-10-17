import SwiftUI
import IchorCore

/// Screenshot mode: Go masks IPs, node and context names, plus the extra words. The words
/// apply when the field is submitted or the screen closes, not on every keystroke. And the
/// icons of the Apps inventory: off, only the bundled ones are shown (a third-party request).
struct PrivacySection: View {
    @Environment(AppModel.self) private var model
    @AppStorage(AppIconSettings.remoteKey) private var remoteIcons = false
    @AppStorage(AuditEventKeys.enabled) private var auditEvents = false
    @State private var words = ""
    @FocusState private var editingWords: Bool

    var body: some View {
        Section {
            Toggle(isOn: Binding(get: { model.privacyMask }, set: { on in
                Task { await model.setPrivacyMask(on) }
            })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Screenshot mode")
                    Text("Hide IP addresses, node names and app logos, e.g. for screenshots or screen sharing")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            // No second flip while the names switch (AppModel.maskTransition).
            .disabled(model.maskTransition)
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
            Toggle(isOn: $auditEvents) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Record actions in the cluster")
                    Text("Also write a Kubernetes Event (reason IchorAction) on each object the app changes, so the cluster’s own history shows what the phone did: kubectl get events, the Argo CD or Lens timelines. Only for Kubernetes objects: node reboots and Talos changes send nothing. Best effort, never blocks an action.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .onChange(of: auditEvents) { _, on in TalosClient.setAuditEvents(on) }
            Toggle(isOn: $remoteIcons) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Download missing app icons")
                    Text("About 250 common apps have bundled icons. When on, icons for other recognised apps are downloaded from jsDelivr (Dashboard Icons). Only the public icon name is sent, never your image names or cluster details. Icons an Argo CD app links in its ichor.levis.name/icon annotation are downloaded too.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            // Off: what was kept is deleted (AppModel.setKeepLastKnown).
            Toggle(isOn: Binding(get: { model.keepLastKnown }, set: { model.setKeepLastKnown($0) })) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Keep last known state")
                    Text("Save the last data fetched from each cluster on this phone, encrypted, so it still shows when the cluster can't be reached. Kept for 24 hours and never backed up.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Privacy")
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
