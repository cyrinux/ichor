import SwiftUI
import IchorCore

/// Chooses how an etcd snapshot is protected before it is taken: age public keys (age, SSH or
/// YubiKey age1tag1 keys; the default when keys are saved), an age passphrase, or nothing.
struct SnapshotProtectionSheet: View {
    let hostname: String
    let savedKeys: String
    var confirm: (SnapshotEncryption) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var mode: SnapshotMode = .passphrase
    @State private var keys = ""
    @State private var recipients: [SnapshotRecipient] = []
    @State private var keysError: String?
    // Not persisted anywhere: the passphrase only lives in this sheet and the transfer.
    @State private var passphrase = ""
    @State private var again = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Protection", selection: $mode) {
                        Text("Public keys").tag(SnapshotMode.keys)
                        Text("Passphrase").tag(SnapshotMode.passphrase)
                        Text("None").tag(SnapshotMode.none)
                    }
                    .pickerStyle(.segmented)
                } footer: {
                    Text("The snapshot from \(hostname) contains every Kubernetes Secret. Encrypt it with age: it can then be restored from any Unix machine with the age and talosctl commands.")
                }
                switch mode {
                case .keys: keysSection
                case .passphrase: passphraseSection
                case .none:
                    Section {
                        Text("The snapshot contains every Kubernetes Secret in clear. Anyone with the file can read them: keep it somewhere safe and delete it when no longer needed.")
                            .foregroundStyle(.statusBad)
                    }
                }
            }
            .navigationTitle("Protect the snapshot")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save snapshot") { confirm(encryption) }.disabled(!ready)
                }
            }
            .task(id: keys) { await check() }
            .onAppear {
                keys = savedKeys
                if !savedKeys.isEmpty { mode = .keys }
            }
        }
    }

    private var keysSection: some View {
        Section {
            TextEditor(text: $keys)
                .font(.caption.monospaced())
                .frame(minHeight: 90)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            if let keysError {
                Text(verbatim: keysError).font(.footnote).foregroundStyle(.statusBad)
            } else if !recipients.isEmpty {
                Text("Encrypted for: \(recipients.map(\.label).joined(separator: ", "))")
                    .font(.footnote).foregroundStyle(.statusOK)
            }
        } header: {
            Text("Public keys, one per line")
        } footer: {
            Text("An age key (age1…), an SSH key (ssh-ed25519, ssh-rsa, for example ~/.ssh/id_ed25519.pub) or a YubiKey key (age1tag1…, from age-plugin-yubikey). Only the matching private keys can decrypt the snapshot; the phone cannot.")
        }
    }

    private var passphraseSection: some View {
        Section {
            SecureField("Passphrase", text: $passphrase).textContentType(.newPassword)
            SecureField("Passphrase again", text: $again).textContentType(.newPassword)
            if let problem = passphraseProblem, !passphrase.isEmpty {
                Text(problem).font(.footnote).foregroundStyle(.statusBad)
            }
        } footer: {
            Text("The passphrase is not stored anywhere: without it, the snapshot cannot be restored.")
        }
    }

    private var passphraseProblem: String? {
        switch backupPassphraseProblem(passphrase, again: again) {
        case .tooShort: String(localized: "Use at least \(minSnapshotPassphrase) characters.")
        case .mismatch: String(localized: "The passphrases differ.")
        case nil: nil
        }
    }

    private var ready: Bool {
        switch mode {
        case .keys: return keysError == nil && !recipients.isEmpty
        case .passphrase: return passphraseProblem == nil
        case .none: return true
        }
    }

    private var encryption: SnapshotEncryption {
        switch mode {
        case .keys: return .keys(keys.trimmingCharacters(in: .whitespacesAndNewlines))
        case .passphrase: return .passphrase(passphrase)
        case .none: return .none
        }
    }

    /// Validates the keys with Go, debounced while typing.
    private func check() async {
        let text = keys
        // Until Go answers for this text, nothing may be saved (ready needs recipients).
        recipients = []
        keysError = nil
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        try? await Task.sleep(for: .milliseconds(300))
        guard !Task.isCancelled else { return }
        do {
            let checked = try await TalosClient.checkSnapshotRecipients(text)
            guard !Task.isCancelled else { return } // a newer text is being checked
            recipients = checked
        } catch {
            guard !Task.isCancelled else { return }
            keysError = error.localizedDescription
        }
    }
}
