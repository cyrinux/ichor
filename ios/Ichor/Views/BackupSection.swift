import SwiftUI
import IchorCore
import UniformTypeIdentifiers

extension UTType {
    /// Backups are binary; the extension is shared with the Android app.
    static let ichorBackup = UTType(filenameExtension: backupExtension, conformingTo: .data) ?? .data
}

/// Settings: back up the clusters and settings to a passphrase-sealed file, or restore one over them.
struct BackupSection: View {
    @Environment(AppModel.self) private var model
    @State private var askingPassphrase = false
    @State private var document: BackupDocument?
    @State private var exporting = false
    @State private var message: String?

    var body: some View {
        Section {
            Button("Back up clusters and settings…") { Task { await startBackup() } }
                .disabled(model.yaml == nil)
                .fileExporter(isPresented: $exporting, document: document, contentType: .ichorBackup,
                              defaultFilename: backupFileName(date: Date())) { result in
                    switch result {
                    case .success: message = String(localized: "Backup saved. Keep the passphrase somewhere safe.")
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
        // The exporter opens once the sheet is gone: one modal cannot present over another.
        .sheet(isPresented: $askingPassphrase, onDismiss: { exporting = document != nil }) {
            NewBackupPassphraseSheet { passphrase in
                document = BackupDocument(data: try await AppBackup.create(model: model, passphrase: passphrase))
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
            .fileImporter(isPresented: $importing, allowedContentTypes: [.ichorBackup, .data, .item]) { result in
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
            picked = PickedBackup(data: data)
        } catch {
            onError(error.localizedDescription)
        }
    }
}

private struct PickedBackup: Identifiable {
    let id = UUID()
    let data: Data
}

/// Asks the passphrase that seals a new backup, twice; `seal` runs while the sheet shows progress.
private struct NewBackupPassphraseSheet: View {
    var seal: (String) async throws -> Void
    @Environment(\.dismiss) private var dismiss
    // Not persisted anywhere: the passphrase only lives in this sheet.
    @State private var passphrase = ""
    @State private var again = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("Passphrase", text: $passphrase).textContentType(.newPassword)
                    SecureField("Passphrase again", text: $again).textContentType(.newPassword)
                    if let problem, !again.isEmpty { Text(problem).font(.footnote).foregroundStyle(.red) }
                    if let error { Text(error).font(.footnote).foregroundStyle(.red) }
                } footer: {
                    Text("The backup holds your clusters’ credentials. Choose a passphrase of at least \(backupMinPassphrase) characters; it is needed to restore and cannot be recovered.")
                }
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

    private var problem: String? {
        switch backupPassphraseProblem(passphrase, again: again) {
        case .tooShort: String(localized: "Use at least \(backupMinPassphrase) characters.")
        case .mismatch: String(localized: "The passphrases differ.")
        case nil: nil
        }
    }

    private func submit() async {
        busy = true
        defer { busy = false }
        do {
            try await seal(passphrase)
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
    @Environment(\.dismiss) private var dismiss
    @State private var passphrase = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("Passphrase", text: $passphrase).textContentType(.password)
                    if let error { Text(error).font(.footnote).foregroundStyle(.red) }
                } footer: {
                    Text("Enter the passphrase the backup was encrypted with.")
                }
                if busy { ProgressView() }
            }
            .navigationTitle("Open the backup")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(busy) }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Restore") { Task { await restore() } }.disabled(passphrase.isEmpty || busy)
                }
            }
        }
        .interactiveDismissDisabled(busy)
    }

    private func restore() async {
        busy = true
        defer { busy = false }
        do {
            try await AppBackup.restore(file, passphrase: passphrase, model: model)
            onRestored()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct BackupDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.ichorBackup] }
    let data: Data

    init(data: Data) { self.data = data }

    init(configuration: ReadConfiguration) throws {
        data = configuration.file.regularFileContents ?? Data()
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: data)
    }
}
