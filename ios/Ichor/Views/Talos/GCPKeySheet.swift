import IchorCore
import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// The service account key (pasted or read from a file, never shown back: only its email and
/// project), the custom role to give it, a test, then sealed under its project id.
struct GCPKeySheet: View {
    let onSaved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var key = ""
    @State private var importing = false
    @State private var importError: String?
    @State private var check: LoadState<GCPCheck>?
    @State private var saveError: String?

    // Explicit: the private @State makes the memberwise init private.
    init(onSaved: @escaping (String) -> Void) {
        self.onSaved = onSaved
    }

    private var identity: GCPKeyIdentity? { GCPKeyIdentity(key: key) }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "A service account key does not expire and can do what its roles allow. It stays sealed on this device and is not in backups: prefer a key with only the role Ichor prints."
                    )
                    .font(.footnote).foregroundStyle(.orange)
                    Text(
                        "Create this custom role in the project and grant it to the service account: only the calls Ichor makes. Enable the Compute Engine API; Cloud Resource Manager lets Test check the role, and Cloud Billing shows the price."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Button("Copy the role definition") {
                        UIPasteboard.general.string = TalosClient.gcpRole(project: identity?.projectId ?? "")
                    }
                }
                Section {
                    Button("Paste the key") {
                        set(UIPasteboard.general.string ?? "")
                    }
                    Button {
                        importing = true
                    } label: {
                        Label("Choose file", systemImage: "doc")
                    }
                    if let identity {
                        Text("Key of \(identity.clientEmail), project \(identity.projectId).")
                            .font(.footnote).foregroundStyle(.secondary)
                    } else if !key.isEmpty {
                        Text("This is not a service account key (a JSON file of type service_account).")
                            .font(.footnote).foregroundStyle(.orange)
                    }
                    if let importError {
                        Text(verbatim: importError).font(.footnote).foregroundStyle(.orange)
                    }
                    Button("Test") { Task { await test() } }
                        .disabled(identity == nil)
                } header: {
                    Text("Service account key (JSON)")
                }
                if let check {
                    Section { result(check) }
                }
                if let saveError {
                    Section { Text(verbatim: saveError).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add a service account key")
            .navigationBarTitleDisplayMode(.inline)
            .fileImporter(isPresented: $importing, allowedContentTypes: [.json, .plainText, .data]) { result in
                if case .success(let url) = result { read(url) }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(!canSave)
                }
            }
        }
    }

    @ViewBuilder
    private func result(_ state: LoadState<GCPCheck>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
            if let link = GCPNames.activationLink(in: error) {
                Link("Open the page to enable it", destination: link)
            }
        case .loaded(let value, _, _):
            let missing = value.missing.joined(separator: ", ")
            if !value.checked {
                Text(
                    "\(value.account) in \(value.project). Whether it may create VMs shows when it tries: enable Cloud Resource Manager to check now."
                )
            } else if value.canCreate {
                Text("\(value.account) may create VMs in \(value.project).")
                    .foregroundStyle(.statusOK)
                if !value.missing.isEmpty {
                    Text("Not granted yet, used by the rest of the role: \(missing)")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            } else {
                Text("\(value.account) may not create VMs in \(value.project). Grant it the role; missing: \(missing)")
                    .foregroundStyle(.orange)
            }
        }
    }

    private var tested: GCPCheck? {
        guard case .loaded(let value, _, _) = check else { return nil }
        return value
    }

    private var canSave: Bool {
        guard let tested, tested.canCreate || !tested.checked else { return false }
        return !tested.project.isEmpty
    }

    private func set(_ text: String) {
        key = text
        check = nil
        importError = nil
    }

    private func read(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        // A service account key is a few kB.
        guard let data = IncomingFile.read(url, limit: 64 * 1024),
            let text = String(data: data, encoding: .utf8)
        else {
            importError = String(localized: "This file cannot be read.")
            return
        }
        set(text)
    }

    private func credentialJSON() -> String {
        let credential = GCPCredential(
            serviceAccountJson: key.trimmingCharacters(in: .whitespacesAndNewlines),
            project: identity?.projectId ?? "")
        return (try? JSONEncoder().encode(credential)).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }

    private func test() async {
        let credential = credentialJSON()
        check = .loading
        check = await LoadState.from { try await TalosClient.gcpTest(credential: credential) }
    }

    private func save() {
        guard let project = tested?.project else { return }
        do {
            try CloudProviderStore.save(provider: CloudProviderStore.gcp, project: project, token: credentialJSON())
            onSaved(project)
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}
