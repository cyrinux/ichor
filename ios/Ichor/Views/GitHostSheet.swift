import SwiftUI
import UIKit
import IchorCore

/// Adds a Git host or changes one: the host (fixed once saved), its kind and its token. Check
/// asks the host who the token signs in as before saving; in the demo it answers without
/// network. A saved token is never shown: an empty field keeps it.
struct GitHostSheet: View {
    /// Nil to add a host.
    let existing: GitHost?

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var store
    @State private var host: String
    @State private var kind: GitHostKind
    @State private var aliases: String
    @State private var token = ""
    @State private var checking = false
    @State private var result: GitHostCheck?
    @State private var failure: String?
    @State private var saveFailure: String?

    // Explicit: the private @State makes the memberwise init private.
    init(existing: GitHost?) {
        self.existing = existing
        _host = State(initialValue: existing?.host ?? "github.com")
        _kind = State(initialValue: existing?.kind ?? .auto)
        _aliases = State(initialValue: existing?.aliases.joined(separator: ", ") ?? "")
    }

    private var normalized: String { normalizedGitHost(host) }
    private var parsedAliases: [String] { parseGitHostAliases(aliases, host: normalized) }
    private var edited: GitHost { GitHost(host: normalized, kind: kind, aliases: parsedAliases) }
    private var typedToken: String { token.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var hasSavedToken: Bool { existing.map { store.hasToken($0.host) } ?? false }
    private var demo: Bool { model.activeSummary?.demo == true }
    private var canCheck: Bool { !normalized.isEmpty && (!typedToken.isEmpty || hasSavedToken || demo) }
    private var canSave: Bool { !normalized.isEmpty && (!typedToken.isEmpty || hasSavedToken) && !checking }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    if let existing {
                        LabeledContent("Host") { Text(verbatim: existing.host) }
                    } else {
                        TextField("Host", text: $host)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.URL)
                    }
                    Picker("Kind", selection: $kind) {
                        ForEach(GitHostKind.allCases, id: \.self) { GitHostKindLabel(kind: $0).tag($0) }
                    }
                    tokenRow
                }
                Section {
                    TextField("Also reached as", text: $aliases, axis: .vertical)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                } footer: {
                    Text(
                        "Other names Argo CD or Flux use for this server, such as an SSH or in-cluster address. Separate them with commas. The token is still only sent to the host above."
                    )
                }
                Section {
                    Button {
                        Task { await check() }
                    } label: {
                        HStack {
                            if checking { ProgressView() }
                            checking ? Text("Checking…") : Text("Check")
                        }
                    }
                    .disabled(checking || !canCheck)
                    checkLine
                }
                if let saveFailure {
                    Section {
                        Label {
                            Text(verbatim: saveFailure).textSelection(.enabled)
                        } icon: {
                            Image(systemName: "exclamationmark.triangle.fill")
                        }
                        .foregroundStyle(.statusBad)
                    }
                }
            }
            .navigationTitle(existing == nil ? Text("Add a Git host") : Text(verbatim: existing?.host ?? ""))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) { Button("Save") { save() }.disabled(!canSave) }
            }
            // Another host, kind or token: the last answer was about something else.
            .onChange(of: host) { resetCheck() }
            .onChange(of: kind) { resetCheck() }
            .onChange(of: token) { resetCheck() }
        }
    }

    private var tokenRow: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                SecureField("Token", text: $token)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .textContentType(.password)
                Button("Paste") { token = UIPasteboard.general.string ?? token }
                    .buttonStyle(.borderless)
            }
            if hasSavedToken, typedToken.isEmpty {
                Text("A token is saved. Type a new one to replace it.")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private var checkLine: some View {
        if let failure {
            Text("Check failed: \(failure)").font(.footnote).foregroundStyle(.statusBad)
        } else if let result {
            VStack(alignment: .leading, spacing: 4) {
                Label {
                    Text("Signed in as \(result.login)")
                } icon: {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
                }
                if !result.scopesOK || !result.missing.isEmpty {
                    Text("Missing scopes: \(result.missing.joined(separator: ", "))")
                        .font(.footnote).foregroundStyle(.statusWarn)
                }
            }
        } else {
            Text("Not checked").font(.footnote).foregroundStyle(.secondary)
        }
    }

    private func check() async {
        guard !checking else { return }
        checking = true
        defer { checking = false }
        failure = nil
        result = nil
        do {
            result = try await store.check(edited, token: typedToken, demo: demo)
        } catch {
            failure = error.localizedDescription
        }
    }

    private func save() {
        // The demo's login is not the token's: keep only a real answer.
        let login = demo ? nil : result?.login
        do {
            try store.save(edited, token: typedToken, login: login)
            dismiss()
        } catch {
            saveFailure = error.localizedDescription
        }
    }

    private func resetCheck() {
        result = nil
        failure = nil
        saveFailure = nil
    }
}
