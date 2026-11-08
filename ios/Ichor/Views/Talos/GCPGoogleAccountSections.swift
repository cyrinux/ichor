import IchorCore
import SwiftUI
import UIKit

/// A credential JSON to seal under its project, and the test it passed.
struct GCPSignedIn {
    let credential: String
    let check: GCPCheck
}

/// The Google-account side of the Google Cloud sheet, as the GKE discovery signs in: the kind of
/// sign-in (the build's own Google client, gcloud's, an OAuth client of your own, a workforce
/// login config), the browser, the quota project to take, then the same test as a key.
/// `onReady` gets the credential to seal once it passed, nil meanwhile. The session stays in that
/// JSON, never on screen.
struct GCPGoogleAccountSections: View {
    let onReady: (GCPSignedIn?) -> Void

    private enum Step {
        case form, working, signingIn
        case projects(GCPSignInResult)
        case failed(String)
        case chosen(GCPSignInResult, LoadState<GCPCheck>)
    }

    @Environment(\.openURL) private var openURL
    @State private var choices: [String] = []
    @State private var kind = ""
    @State private var clientId = ""
    @State private var clientSecret = ""
    @State private var redirectURL = ""
    @State private var loginConfig = ""
    @State private var typedProject = ""
    @State private var step = Step.form
    @State private var flow = KubeSignInFlow()
    @State private var task: Task<Void, Never>?

    init(onReady: @escaping (GCPSignedIn?) -> Void) {
        self.onReady = onReady
    }

    var body: some View {
        Section {
            Text(
                "The sign-in can do what your account may on every project, not only the role Ichor prints. It stays sealed on this device and is not in backups: prefer a key with only that role when you have one."
            )
            .font(.footnote).foregroundStyle(.orange)
            // On this row, always shown: a List gives a Section's modifiers to each of its rows,
            // so steps coming and going would cancel the sign-in or take the credential twice.
            .task { await loadChoices() }
            .onChange(of: flow.phase) { _, phase in
                followFlow(phase)
            }
            // The sheet closed or switched to a key: the wait for the browser ends.
            .onDisappear { cancel() }
            stepRows
        }
    }

    @ViewBuilder private var stepRows: some View {
        switch step {
        case .form:
            formRows
        case .working:
            ProgressView().frame(maxWidth: .infinity)
        case .signingIn:
            if case .device(let prompt) = flow.phase {
                deviceRows(prompt)
            } else {
                CloudDiscoverySignInRows(flow: flow) {
                    stopSignIn()
                }
            }
        case .projects(let result):
            projectRows(result)
        case .failed(let message):
            Text(verbatim: message).foregroundStyle(.statusBad)
            Button("Retry") { step = .form }
        case .chosen(let result, let check):
            Text("Signed in with Google as \(result.account)").font(.footnote)
            GCPCheckRows(state: check)
            if case .loaded(let value, _, _) = check, !value.renewHint.isEmpty {
                Text("Once Google ends the sign-in: \(value.renewHint).")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Button("Pick another project") {
                onReady(nil)
                step = .projects(result)
            }
        }
    }

    @ViewBuilder private var formRows: some View {
        if choices.count > 1 {
            Picker("Sign-in", selection: $kind) {
                ForEach(choices, id: \.self) { Text(verbatim: label(of: $0)).tag($0) }
            }
        }
        switch kind {
        case "oauth":
            Text(
                "Ask your Google Cloud admin for a Desktop app OAuth client (APIs & Services → Credentials) with an Internal consent screen. Sign in then opens your Google account in the browser."
            )
            .font(.footnote).foregroundStyle(.secondary)
            plainField("OAuth client ID", text: $clientId)
            SecureField("OAuth client secret", text: $clientSecret)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            plainField("Redirect URL (optional)", text: $redirectURL)
        case "workforce":
            Text(
                "Import the login config your administrator gave you; the sign-in opens your organisation's identity provider in the browser."
            )
            .font(.footnote).foregroundStyle(.secondary)
            TextField("Workforce login config (JSON)", text: $loginConfig, axis: .vertical)
                .lineLimit(3...8)
                .font(.footnote.monospaced())
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("Paste") {
                loginConfig = UIPasteboard.general.string ?? ""
            }
        default:
            EmptyView()
        }
        Button("Sign in") { signIn() }
            .disabled(!canSignIn)
    }

    private func plainField(_ title: LocalizedStringKey, text: Binding<String>) -> some View {
        TextField(title, text: text)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
    }

    @ViewBuilder
    private func projectRows(_ result: GCPSignInResult) -> some View {
        Text("Signed in with Google as \(result.account)").font(.footnote)
        Text("The project the calls are billed to (the quota project):")
            .font(.subheadline.weight(.semibold))
        ForEach(result.projects) { project in
            Button {
                task = Task { await choose(result, project.id) }
            } label: {
                Text(verbatim: project.label)
            }
        }
        if result.more || result.projects.isEmpty {
            plainField("Project ID", text: $typedProject)
            Button("Use this project") {
                let project = trimmed(typedProject)
                task = Task { await choose(result, project) }
            }
            .disabled(trimmed(typedProject).isEmpty)
        }
    }

    @ViewBuilder
    private func deviceRows(_ prompt: KubeSignInPrompt) -> some View {
        Text("Enter this code on the sign-in page:")
        Text(verbatim: prompt.userCode ?? "")
            .font(.system(.largeTitle, design: .monospaced).weight(.semibold))
            .textSelection(.enabled)
            .frame(maxWidth: .infinity)
        Button {
            UIPasteboard.general.string = prompt.userCode
        } label: {
            Label("Copy code", systemImage: "doc.on.doc")
        }
        if let url = URL(string: prompt.openURL) {
            Button {
                openURL(url)
            } label: {
                Label("Open the sign-in page", systemImage: "safari")
            }
        }
        HStack(spacing: 12) {
            ProgressView()
            Text("Waiting for you to approve the sign-in…").font(.footnote).foregroundStyle(.secondary)
        }
        Button("Cancel", role: .cancel) { stopSignIn() }
    }

    private func label(of kind: String) -> String {
        switch kind {
        case "native": String(localized: "Your Google account (Ichor's sign-in)")
        case "gcloud": String(localized: "Your Google account (like gcloud)")
        case "oauth": String(localized: "Your organisation's OAuth client")
        case "workforce": String(localized: "Workforce identity (login config)")
        default: kind
        }
    }

    private var canSignIn: Bool {
        switch kind {
        case "": false
        case "oauth": !trimmed(clientId).isEmpty
        case "workforce": !trimmed(loginConfig).isEmpty
        default: true
        }
    }

    private func trimmed(_ text: String) -> String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The kind's fields as GCPSignInStart takes them; "" for those with none.
    private func secretsJSON() -> String {
        let fields: [String: String]
        switch kind {
        case "oauth":
            fields = [
                "gcpOAuthClientId": trimmed(clientId), "gcpOAuthClientSecret": trimmed(clientSecret),
                "gcpOAuthRedirectUrl": trimmed(redirectURL),
            ]
        case "workforce":
            fields = ["gcpLoginConfigJson": trimmed(loginConfig)]
        default:
            return ""
        }
        let given = fields.filter { !$0.value.isEmpty }
        return (try? JSONEncoder().encode(given)).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }

    private func loadChoices() async {
        guard choices.isEmpty else { return }
        do {
            choices = try await TalosClient.gcpSignInChoices()
            if !choices.contains(kind) { kind = choices.first ?? "" }
        } catch {
            step = .failed(error.localizedDescription)
        }
    }

    private func signIn() {
        onReady(nil)
        step = .signingIn
        flow.start(TalosClient.gcpSignInStart(kind: kind, secrets: secretsJSON()))
    }

    /// The browser sign-in ended: its credential and projects, why it failed, or the form again.
    private func followFlow(_ phase: KubeSignInFlow.Phase) {
        guard case .signingIn = step else { return }
        switch phase {
        case .signedIn:
            task = Task { await takeCredential() }
        case .failed(let message):
            step = .failed(message)
        case .idle:
            step = .form
        default:
            break
        }
    }

    private func stopSignIn() {
        cancel()
        step = .form
    }

    private func cancel() {
        flow.cancel()
        task?.cancel()
        TalosClient.forgetDiscoverSignIn()
    }

    private func takeCredential() async {
        step = .working
        do {
            let result = try await TalosClient.gcpSignInCredential()
            if result.projects.count == 1, !result.more, let only = result.projects.first {
                await choose(result, only.id)
            } else {
                step = .projects(result)
            }
        } catch {
            step = .failed(error.localizedDescription)
        }
    }

    /// The sign-in bound to `project` (edited as JSON: the struct would drop the session), tested.
    private func choose(_ result: GCPSignInResult, _ project: String) async {
        step = .working
        let bound = GCPCredential.withProject(result.credential, project: project) ?? result.credential
        let check = await LoadState.from { try await TalosClient.gcpTest(credential: bound) }
        if case .loaded(let tested, _, _) = check {
            // The test renewed the session: seal the new one.
            let sealed = GCPCredential.renewed(bound, from: tested.credential) ?? bound
            onReady(GCPSignedIn(credential: sealed, check: tested))
        }
        step = .chosen(result, check)
    }
}
