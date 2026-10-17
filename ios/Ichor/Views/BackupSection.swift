import SwiftUI
import IchorCore
import UniformTypeIdentifiers

extension UTType {
    /// Backups are binary; the extension is shared with the Android app.
    static let ichorBackup = UTType(filenameExtension: backupExtension, conformingTo: .data) ?? .data
    /// A standard age file (`age -d` opens it), the backup's other export format.
    static let ageBackup = UTType(filenameExtension: backupAgeExtension, conformingTo: .data) ?? .data
}

/// Settings: back up the clusters and settings to a passphrase-sealed file, or restore one over them.
struct BackupSection: View {
    /// Set from elsewhere in Settings (the delete confirmation's "Back up first") to start a backup.
    @Binding var requested: Bool
    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai
    @Environment(GitHostStore.self) private var gitHosts
    @State private var askingPassphrase = false
    @State private var document: BackupDocument?
    /// The format and sealing of the backup being exported: its file type, name and saved message.
    @State private var sealed = BackupSeal.ichor
    @State private var exporting = false
    @State private var message: String?

    // Explicit: the private @State makes the memberwise init private.
    init(requested: Binding<Bool> = .constant(false)) {
        _requested = requested
    }

    var body: some View {
        Section {
            Button("Back up clusters and settings…") { Task { await startBackup() } }
                .disabled(!model.hasConfig)
                .fileExporter(isPresented: $exporting, document: document,
                              contentType: sealed.format == .age ? .ageBackup : .ichorBackup,
                              defaultFilename: backupFileName(date: Date(), format: sealed.format)) { result in
                    switch result {
                    case .success:
                        message = sealed.forRecipients
                            ? String(localized: "Backup saved. It opens with your age key on a computer.")
                            : String(localized: "Backup saved. Keep the passphrase somewhere safe.")
                    case .failure(let error): message = error.localizedDescription
                    }
                    document = nil // don't keep the sealed credentials around
                }
            RestoreBackupButton(confirmFirst: true) {
                message = String(localized: "Backup restored.")
            } onError: { message = $0 }
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        } header: {
            Text("Backup")
        } footer: {
            Text("Save your clusters and settings to a file encrypted with a passphrase, to restore them on a new phone (iOS or Android). Without the passphrase the file cannot be opened.")
        }
        .onChange(of: requested) { _, now in
            guard now else { return }
            requested = false
            Task { await startBackup() }
        }
        // The exporter opens once the sheet is gone: one modal cannot present over another.
        .sheet(isPresented: $askingPassphrase, onDismiss: { exporting = document != nil }) {
            NewBackupPassphraseSheet { seal, includeSecrets in
                let data = try await AppBackup.create(
                    model: model, ai: ai, gitHosts: gitHosts, passphrase: seal.passphrase,
                    includeSecrets: includeSecrets, format: seal.format, recipients: seal.recipients)
                sealed = BackupSeal(format: seal.format, passphrase: "", recipients: seal.recipients)
                document = BackupDocument(data: data)
            }
        }
    }

    /// The file holds the clusters' credentials: with the app lock on, prove it is the owner first.
    private func startBackup() async {
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Back up clusters and their credentials")) {
            message = failure
            return
        }
        message = nil
        askingPassphrase = true
    }
}

/// Picks a backup file and asks its passphrase, then restores it (replacing the stored config).
/// With `confirmFirst` (clusters already stored) it says what a restore replaces before picking.
struct RestoreBackupButton: View {
    var confirmFirst: Bool
    var onRestored: () -> Void
    var onError: (String) -> Void

    @State private var confirming = false
    @State private var importing = false
    @State private var picked: PickedBackup?

    var body: some View {
        Button("Restore a backup…") { if confirmFirst { confirming = true } else { importing = true } }
            .alert("Restore a backup", isPresented: $confirming) {
                Button("Choose file") { importing = true }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Restoring replaces every cluster and setting on this device with those of the backup.")
            }
            .fileImporter(isPresented: $importing, allowedContentTypes: [.ichorBackup, .ageBackup, .data, .item]) { result in
                switch result {
                case .success(let url): read(url)
                case .failure(let error): onError(error.localizedDescription)
                }
            }
            .sheet(item: $picked) { backup in
                UnlockBackupSheet(file: backup.data) {
                    picked = nil
                    onRestored()
                }
            }
    }

    private func read(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        do {
            let data = try Data(contentsOf: url)
            // A backup is a few KB; anything much bigger is not one.
            guard data.count <= 5 * 1024 * 1024 else { throw BackupError.notBackup }
            Task { await offer(data) }
        } catch {
            onError(error.localizedDescription)
        }
    }

    /// An age file sealed for age keys only cannot open here: say so rather than ask a passphrase.
    private func offer(_ data: Data) async {
        if await AppBackup.needsIdentity(data) {
            onError(BackupError.needsIdentity.localizedDescription)
        } else {
            picked = PickedBackup(data: data)
        }
    }
}

private struct PickedBackup: Identifiable {
    let id = UUID()
    let data: Data
}

/// How a new backup is sealed: the file format and either a passphrase or, for an age file
/// only, age public keys (one per line).
struct BackupSeal: Sendable {
    var format: BackupFileFormat
    var passphrase: String
    var recipients: String

    static let ichor = BackupSeal(format: .ichor, passphrase: "", recipients: "")

    /// An age file for public keys: only their age keys open it, on a computer.
    var forRecipients: Bool { format == .age && passphrase.isEmpty && !recipients.isEmpty }
}

/// Asks the format of a new backup, its passphrase (twice) or, for an age file, the age public
/// keys it is sealed for, and whether to include the API keys and Git tokens (off by default);
/// `seal` runs while the sheet shows progress.
private struct NewBackupPassphraseSheet: View {
    var seal: (_ seal: BackupSeal, _ includeSecrets: Bool) async throws -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var format: BackupFileFormat = .ichor
    @State private var ageMode: BackupAgeMode = .passphrase
    // Not persisted anywhere: the passphrase only lives in this sheet.
    @State private var passphrase = ""
    @State private var again = ""
    @State private var recipients = ""
    @State private var includeSecrets = false
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                formatSection
                if forRecipients { recipientsSection } else { passphraseSection }
                Section {
                    Toggle("Include API keys and Git tokens", isOn: $includeSecrets)
                        .disabled(busy)
                } footer: {
                    if forRecipients {
                        Text("Anyone with the file and a matching age key can use them.")
                    } else {
                        Text("Anyone with the file and its passphrase can use them.")
                    }
                }
                if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
                if busy { ProgressView("Encrypting…") }
            }
            .navigationTitle("Encrypt the backup")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(busy) }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Encrypt and save") { Task { await submit() } }.disabled(problem != nil || busy)
                }
            }
        }
        .interactiveDismissDisabled(busy)
    }

    private var formatSection: some View {
        Section {
            Picker("Format", selection: $format) {
                Text("Ichor format (default)").tag(BackupFileFormat.ichor)
                Text("age file").tag(BackupFileFormat.age)
            }
            .disabled(busy)
            if format == .age {
                Picker("Protection", selection: $ageMode) {
                    Text("Passphrase").tag(BackupAgeMode.passphrase)
                    Text("age recipients").tag(BackupAgeMode.recipients)
                }
                .pickerStyle(.segmented)
                .disabled(busy)
            }
        } footer: {
            if format == .age {
                Text("An age file opens on a computer with the age command (age -d). Sealed with a passphrase, it also restores in Ichor on iOS or Android.")
            }
        }
    }

    private var passphraseSection: some View {
        Section {
            SecureField("Passphrase", text: $passphrase).textContentType(.newPassword)
            SecureField("Passphrase again", text: $again).textContentType(.newPassword)
            if let problem, !again.isEmpty { Text(problem).font(.footnote).foregroundStyle(.statusBad) }
        } footer: {
            Text("The backup holds your clusters’ credentials. Choose a passphrase of at least \(backupMinPassphrase) characters; it is needed to restore and cannot be recovered.")
        }
    }

    private var recipientsSection: some View {
        Section {
            TextEditor(text: $recipients)
                .font(.caption.monospaced())
                .frame(minHeight: 90)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .accessibilityLabel("age recipients")
                .disabled(busy)
            if let problem, !recipients.isEmpty { Text(problem).font(.footnote).foregroundStyle(.statusBad) }
        } header: {
            Text("age recipients")
        } footer: {
            Text("One age public key (age1…) per line. Only the matching age keys open the file, on a computer: this phone cannot restore it.")
        }
    }

    private var forRecipients: Bool { format == .age && ageMode == .recipients }

    private var problem: String? {
        if forRecipients {
            switch ageRecipientsProblem(recipients) {
            case .empty: return String(localized: "Paste at least one age public key.")
            case .invalid(let line): return String(localized: "Line \(line) is not an age public key (age1…).")
            case nil: return nil
            }
        }
        switch backupPassphraseProblem(passphrase, again: again) {
        case .tooShort: return String(localized: "Use at least \(backupMinPassphrase) characters.")
        case .mismatch: return String(localized: "The passphrases differ.")
        case nil: return nil
        }
    }

    private func submit() async {
        busy = true
        defer { busy = false }
        error = nil
        // Exactly one of the passphrase and the keys: age refuses a passphrase with other recipients.
        let sealing = forRecipients
            ? BackupSeal(format: .age, passphrase: "", recipients: recipients)
            : BackupSeal(format: format, passphrase: passphrase, recipients: "")
        do {
            try await seal(sealing, includeSecrets)
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// Asks a picked backup's passphrase and restores it; stays open on a wrong passphrase.
private struct UnlockBackupSheet: View {
    let file: Data
    var onRestored: () -> Void
    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai
    @Environment(GitHostStore.self) private var gitHosts
    @Environment(\.dismiss) private var dismiss
    @State private var passphrase = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("Passphrase", text: $passphrase).textContentType(.password)
                    if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
                } footer: {
                    Text("Enter the passphrase the backup was encrypted with.")
                }
            }
            .navigationTitle("Open the backup")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(busy) }
                ToolbarItem(placement: .confirmationAction) {
                    // The progress takes the place of the button just pressed, so the form keeps its layout.
                    if busy {
                        ProgressView().accessibilityLabel("Restoring…")
                    } else {
                        Button("Restore") { Task { await restore() } }.disabled(passphrase.isEmpty)
                    }
                }
            }
        }
        .interactiveDismissDisabled(busy)
    }

    private func restore() async {
        busy = true
        defer { busy = false }
        do {
            try await AppBackup.restore(file, passphrase: passphrase, model: model, ai: ai, gitHosts: gitHosts)
            onRestored()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct BackupDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.ichorBackup, .ageBackup] }
    let data: Data

    init(data: Data) { self.data = data }

    init(configuration: ReadConfiguration) throws {
        data = configuration.file.regularFileContents ?? Data()
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: data)
    }
}
