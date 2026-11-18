import IchorCore
import SwiftUI

/// What the bastion sheet acts on: a cluster added from a kubeconfig, and the problem the
/// screen that opened it hit (a changed host key opens on the pin replacement).
struct KubeBastionTarget: Identifiable, Equatable {
    let kube: String
    let context: String
    var problem: BastionProblem?

    var id: String { context }
}

/// The SSH bastion of a kubeconfig cluster: host, user and a key or a password; the host key
/// shown to compare before it is trusted; when set, edit (empty secrets kept), replace a changed
/// host key, or remove. Secrets live in @State only while the sheet is open, never in scene
/// storage, and are cleared when it closes.
struct KubeBastionSheet: View {
    let target: KubeBastionTarget
    var onChanged: () -> Void = {}

    @Environment(\.dismiss) private var dismiss
    @State private var info: KubeBastionInfo?
    @State private var loading = true
    @State private var host = ""
    @State private var user = ""
    @State private var useKey = true
    @State private var privateKey = ""
    @State private var passphrase = ""
    @State private var password = ""
    @State private var checked: KubeBastionHostKey?
    @State private var pinMismatch = false
    @State private var busy = false
    @State private var error: String?
    @State private var confirmRemove = false
    @State private var pickingKey = false

    private var configured: Bool { info?.configured == true }

    /// A host other than the configured one is a new bastion: its key is checked first.
    private var needsCheck: Bool {
        guard configured, let stored = info?.host else { return true }
        let typed = host.trimmingCharacters(in: .whitespacesAndNewlines)
        return !typed.isEmpty && typed != stored && "\(typed):22" != stored
    }

    private var canSave: Bool {
        let blank = { (s: String) in s.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
        let credential = useKey ? !blank(privateKey) : !password.isEmpty
        return !busy && !blank(host) && !blank(user) && (credential || configured) && (!needsCheck || checked != nil)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "Reach this cluster's API server through an SSH jump host. The bastion sees this phone's address and the API server's address, never your credentials or what goes through."
                    )
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                }
                if loading {
                    ProgressView()
                } else if configured && pinMismatch {
                    replacePin
                } else {
                    form
                }
                if let error {
                    Section {
                        Text(error).font(.footnote).foregroundStyle(.statusBad)
                    }
                }
            }
            .navigationTitle("SSH bastion")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .disabled(busy)
        }
        .task { await load() }
        .onDisappear(perform: clearSecrets)
        .confirmationDialog(
            "Remove the SSH bastion? The cluster connects directly again.", isPresented: $confirmRemove,
            titleVisibility: .visible
        ) {
            Button("Remove", role: .destructive) { act { try await remove() } }
        }
        .fileImporter(isPresented: $pickingKey, allowedContentTypes: [.data, .plainText, .item]) { result in
            if case .success(let url) = result {
                readKey(url)
            }
        }
    }

    @ViewBuilder private var form: some View {
        if let info, info.configured {
            Section("Pinned host key") {
                hostKeyLines(type: info.hostKeyType, fingerprint: info.hostKeyFingerprint)
            }
        }
        Section {
            TextField("Bastion host (host or host:port)", text: $host)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .onChange(of: host) { checked = nil }
            TextField("User", text: $user)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Picker("SSH bastion", selection: $useKey) {
                Text("Private key").tag(true)
                Text("Password").tag(false)
            }
            .pickerStyle(.segmented)
            if useKey {
                keyField
                SecureField("Passphrase (if the key has one)", text: $passphrase)
            } else {
                SecureField("Password", text: $password)
            }
        } footer: {
            if configured {
                Text("Leave the secrets empty to keep the stored ones")
            }
        }
        if needsCheck {
            Section("Host key") {
                if let checked {
                    hostKeyLines(type: checked.hostKeyType, fingerprint: checked.hostKeyFingerprint)
                    Text("Compare it with the bastion's own before trusting it.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                } else {
                    Button("Check host key") { act { try await checkHostKey() } }
                        .disabled(host.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
        Section {
            Button(needsCheck ? "Trust and save" : "Save") { act { try await save() } }
                .disabled(!canSave)
            if configured {
                Button("Remove", role: .destructive) { confirmRemove = true }
            }
        }
    }

    /// The private key: pasted or picked from a file, never shown once entered.
    @ViewBuilder private var keyField: some View {
        if privateKey.isEmpty {
            SecureField("Paste the private key", text: $privateKey)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button {
                pickingKey = true
            } label: {
                Label("Pick a key file", systemImage: "doc")
            }
        } else {
            HStack {
                Label("Private key", systemImage: "key")
                Spacer()
                Button("Remove", role: .destructive) { privateKey = "" }
                    .buttonStyle(.borderless)
            }
        }
    }

    /// The pinned key no longer matches: the old and the new fingerprints, then trust the new.
    @ViewBuilder private var replacePin: some View {
        Section {
            Text(
                "The SSH bastion presented a different host key. Nothing goes through until you check and trust the new one."
            )
            .foregroundStyle(.statusBad)
        }
        if let info {
            Section("Pinned key") {
                hostKeyLines(type: info.hostKeyType, fingerprint: info.hostKeyFingerprint)
            }
        }
        if let checked {
            Section {
                hostKeyLines(type: checked.hostKeyType, fingerprint: checked.hostKeyFingerprint)
            } header: {
                Text("Key the bastion presents now")
            } footer: {
                Text("Compare it with the bastion's own before trusting it.")
            }
            Section {
                Button("Trust the new key") { act { try await replacePinnedKey(checked) } }
            }
        } else {
            Section {
                Button("Check the new host key") { act { try await checkHostKey() } }
            }
        }
    }

    private func hostKeyLines(type: String, fingerprint: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: type)
            Text(verbatim: fingerprint).textSelection(.enabled)
        }
        .font(.system(.footnote, design: .monospaced))
    }

    private func load() async {
        do {
            let loaded = try await TalosClient.bastionInfo(kube: target.kube, context: target.context)
            info = loaded
            host = loaded.host
            user = loaded.user
            useKey = loaded.usesKey
            pinMismatch = target.problem == .hostKey
        } catch {
            self.error = bastionErrorText(error.localizedDescription)
        }
        loading = false
    }

    private func checkHostKey() async throws {
        let typed = host.trimmingCharacters(in: .whitespacesAndNewlines)
        checked = try await TalosClient.bastionHostKey(host: typed.isEmpty ? info?.host ?? "" : typed)
    }

    private func save() async throws {
        let fields = BastionFields(
            host: host, user: user,
            privateKey: useKey ? privateKey.trimmingCharacters(in: .whitespacesAndNewlines) : "",
            passphrase: useKey ? passphrase : "",
            password: useKey ? "" : password,
            hostKey: checked?.hostKey ?? "")
        _ = try await TalosClient.setBastion(kube: target.kube, context: target.context, fields: fields)
        finish()
    }

    private func replacePinnedKey(_ key: KubeBastionHostKey) async throws {
        _ = try await TalosClient.setBastion(
            kube: target.kube, context: target.context, fields: BastionFields(hostKey: key.hostKey))
        finish()
    }

    private func remove() async throws {
        try await TalosClient.removeBastion(kube: target.kube, context: target.context)
        finish()
    }

    private func finish() {
        clearSecrets()
        onChanged()
        dismiss()
    }

    private func act(_ work: @escaping () async throws -> Void) {
        guard !busy else { return }
        busy = true
        error = nil
        Task {
            do {
                try await work()
            } catch {
                let message = error.localizedDescription
                self.error = bastionErrorText(message)
                if bastionProblem(message) == .hostKey {
                    pinMismatch = true
                }
            }
            busy = false
        }
    }

    private func readKey(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer {
            if scoped {
                url.stopAccessingSecurityScopedResource()
            }
        }
        // A private key is a few kB.
        guard let data = IncomingFile.read(url, limit: 64 * 1024),
            let text = String(data: data, encoding: .utf8),
            !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        else {
            error = String(localized: "This file cannot be read.")
            return
        }
        privateKey = text
        error = nil
    }

    private func clearSecrets() {
        privateKey = ""
        passphrase = ""
        password = ""
    }
}

extension BastionProblem {
    /// What failed and where to look, in the app's language.
    var localizedMessage: String {
        switch self {
        case .unreachable:
            String(
                localized: "The SSH bastion does not answer: check its address and that this phone can reach it.")
        case .hostKey:
            String(
                localized:
                    "The SSH bastion presented a different host key. Nothing goes through until you check and trust the new one."
            )
        case .auth: String(localized: "The SSH bastion refused the user, the key or the password.")
        case .key: String(localized: "The private key cannot be read: check it and its passphrase.")
        case .forward:
            String(
                localized: "The SSH bastion would not connect to the API server: it must allow TCP forwarding to it.")
        }
    }
}

extension AppModel {
    /// The bastion sheet's target for `context`: a cluster added from a kubeconfig that can have
    /// one (not the demo, no proxy-url, not in screenshot mode); nil otherwise.
    func bastionTarget(for context: ContextSummary?, problem: BastionProblem? = nil) -> KubeBastionTarget? {
        guard let context, let kubeYAML, bastionOffered(context, masked: labels.masked) else { return nil }
        return KubeBastionTarget(kube: kubeYAML, context: context.name, problem: problem)
    }
}

/// A core error as the bastion screens say it: a bastion problem's sentence, else as is.
func bastionErrorText(_ message: String) -> String {
    bastionProblem(message)?.localizedMessage ?? message
}

/// What a screen shows instead of its data when the cluster's bastion failed: the sentence and
/// the way to its settings.
struct KubeBastionProblemView: View {
    let target: KubeBastionTarget
    let problem: BastionProblem
    let retry: () async -> Void

    @State private var editing: KubeBastionTarget?

    var body: some View {
        ContentUnavailableView {
            Label("SSH bastion", systemImage: "network")
        } description: {
            Text(problem.localizedMessage)
        } actions: {
            Button("Bastion settings") { editing = target }
                .buttonStyle(.borderedProminent)
            Button("Retry") { Task { await retry() } }
        }
        .sheet(item: $editing) { target in
            KubeBastionSheet(target: target) { Task { await retry() } }
        }
    }
}
