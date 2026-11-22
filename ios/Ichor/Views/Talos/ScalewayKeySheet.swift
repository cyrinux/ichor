import IchorCore
import SwiftUI
import UIKit

/// A label, the API key (the secret masked), the IAM policy to attach, a test, then sealed.
struct ScalewayKeySheet: View {
    let onSaved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var label = ""
    @State private var accessKey = ""
    @State private var secret = ""
    @State private var project = ""
    @State private var check: LoadState<ScalewayCheck>?
    @State private var saveError: String?

    // Explicit: the private @State makes the memberwise init private.
    init(onSaved: @escaping (String) -> Void) {
        self.onSaved = onSaved
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "An API key's secret can do what its IAM policy allows until it is revoked. It stays sealed on this device and is not in backups."
                    )
                    .font(.footnote).foregroundStyle(.orange)
                    Text(
                        "Attach this IAM policy to the API key's application at Scaleway: the Instances and Block Storage permission sets, on one project."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Button("Copy the IAM policy") {
                        UIPasteboard.general.string = TalosClient.scalewayPolicy(project: project)
                    }
                }
                Section {
                    TextField("Project name (yours, for this list)", text: $label)
                    TextField("Access key", text: $accessKey)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: accessKey) { check = nil }
                    SecureField("Secret key", text: $secret)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: secret) { check = nil }
                    TextField("Project ID", text: $project)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: project) { check = nil }
                    Button("Test") { Task { await test() } }
                        .disabled(accessKey.isEmpty || secret.isEmpty)
                } footer: {
                    Text("Empty for the key's default project.")
                }
                if let check {
                    Section { result(check) }
                }
                if let saveError {
                    Section { Text(verbatim: saveError).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add a Scaleway API key")
            .navigationBarTitleDisplayMode(.inline)
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
    private func result(_ state: LoadState<ScalewayCheck>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
        case .loaded(let value, _, _):
            if !value.checked {
                Text("Signed in to project \(value.project). Whether it may create Instances shows when it tries.")
            } else if value.canCreate {
                Text("It may create Instances in project \(value.project).").foregroundStyle(.statusOK)
            } else {
                Text("It may not create Instances: attach the policy's permission sets.").foregroundStyle(.orange)
            }
        }
    }

    private var tested: ScalewayCheck? {
        guard case .loaded(let value, _, _) = check else { return nil }
        return value
    }

    private var canSave: Bool {
        guard let tested, tested.canCreate || !tested.checked else { return false }
        return !label.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private func credentialJSON() -> String {
        let credential = ScalewayCredential(
            accessKey: accessKey.trimmingCharacters(in: .whitespacesAndNewlines),
            secretKey: secret.trimmingCharacters(in: .whitespacesAndNewlines),
            project: project.trimmingCharacters(in: .whitespacesAndNewlines), zone: tested?.zone ?? "")
        return (try? JSONEncoder().encode(credential)).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }

    private func test() async {
        let credential = credentialJSON()
        check = .loading
        let state = await LoadState.from { try await TalosClient.scalewayTest(credential: credential) }
        check = state
    }

    private func save() {
        let name = label.trimmingCharacters(in: .whitespaces)
        do {
            try CloudProviderStore.save(provider: CloudProviderStore.scaleway, project: name, token: credentialJSON())
            onSaved(name)
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}
