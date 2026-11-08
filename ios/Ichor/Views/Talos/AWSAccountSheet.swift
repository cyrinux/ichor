import IchorCore
import SwiftUI
import UIKit

/// Access keys (masked, an optional session token, the region) or an IAM Identity Center sign-in,
/// the policy to give them, a test, then sealed under the account id.
struct AWSAccountSheet: View {
    let onSaved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var identityCenter = false
    @State private var accessKeyId = ""
    @State private var secret = ""
    @State private var sessionToken = ""
    @State private var region = ""
    @State private var check: LoadState<AWSCheck>?
    @State private var signedIn: AWSSignedIn?
    @State private var saveError: String?

    // Explicit: the private @State makes the memberwise init private.
    init(onSaved: @escaping (String) -> Void) {
        self.onSaved = onSaved
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Account", selection: $identityCenter) {
                        Text("Access keys").tag(false)
                        Text("IAM Identity Center").tag(true)
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    Text(
                        "Give the keys' IAM user this policy: only the calls Ichor makes, and it may terminate only the instances Ichor tagged."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Button("Copy the IAM policy") {
                        UIPasteboard.general.string = TalosClient.awsPolicy()
                    }
                }
                if identityCenter {
                    AWSIdentityCenterSections { signedIn = $0 }
                } else {
                    keysSections
                }
                if let saveError {
                    Section { Text(verbatim: saveError).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add an AWS account")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(ready == nil)
                }
            }
        }
    }

    @ViewBuilder private var keysSections: some View {
        Section {
            Text(
                "Access keys do not expire unless you rotate them and can do what their IAM policy allows. They stay sealed on this device and are not in backups."
            )
            .font(.footnote).foregroundStyle(.orange)
            TextField("Access key ID", text: $accessKeyId)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onChange(of: accessKeyId) { check = nil }
            SecureField("Secret access key", text: $secret)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onChange(of: secret) { check = nil }
            SecureField("Session token (optional)", text: $sessionToken)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onChange(of: sessionToken) { check = nil }
            TextField("Region", text: $region)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onChange(of: region) { check = nil }
            Button("Test") { Task { await test() } }
                .disabled(accessKeyId.isEmpty || secret.isEmpty || region.isEmpty)
        } footer: {
            Text("From aws sts get-session-token or the SSO portal. It expires: add the keys again then.")
        }
        if let check {
            Section { AWSCheckRows(state: check) }
        }
    }

    /// What the test passed: the keys (renewed by the core when it says so), or the sign-in.
    private var ready: AWSSignedIn? {
        let passed: AWSSignedIn?
        if identityCenter {
            passed = signedIn
        } else if case .loaded(let value, _, _) = check {
            let sealed = value.credential.isEmpty ? credentialJSON() : value.credential
            passed = AWSSignedIn(credential: sealed, check: value)
        } else {
            passed = nil
        }
        guard let passed, passed.check.canCreate || !passed.check.checked, !passed.check.account.isEmpty else {
            return nil
        }
        return passed
    }

    private func credentialJSON() -> String {
        let trim = { (text: String) in text.trimmingCharacters(in: .whitespacesAndNewlines) }
        let credential = AWSCredential(
            accessKeyId: trim(accessKeyId), secretAccessKey: trim(secret), sessionToken: trim(sessionToken),
            region: trim(region))
        return (try? JSONEncoder().encode(credential)).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }

    private func test() async {
        let credential = credentialJSON()
        check = .loading
        check = await LoadState.from { try await TalosClient.awsTest(credential: credential) }
    }

    private func save() {
        guard let ready else { return }
        do {
            try CloudProviderStore.save(
                provider: CloudProviderStore.aws, project: ready.check.account, token: ready.credential)
            onSaved(ready.check.account)
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}

/// A credential JSON to seal, and the test it passed.
struct AWSSignedIn {
    let credential: String
    let check: AWSCheck
}

/// What a test of the keys or the sign-in says: whose they are, and whether they may run instances.
struct AWSCheckRows: View {
    let state: LoadState<AWSCheck>

    var body: some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
        case .loaded(let value, _, _):
            if !value.checked {
                Text(
                    "Keys of account \(value.account), in \(value.region). Whether they may run instances shows when they try."
                )
            } else if value.canCreate {
                Text("Keys of account \(value.account) may run instances in \(value.region).")
                    .foregroundStyle(.statusOK)
            } else {
                Text(
                    "Keys of account \(value.account) may not run instances in \(value.region): give their IAM user the policy (ec2:RunInstances)."
                )
                .foregroundStyle(.orange)
            }
        }
    }
}

/// The IAM Identity Center side of the account sheet, as `aws sso login`: the start URL and regions,
/// the device code to approve in the browser, the account and role to take, then the same test as
/// access keys. `onReady` gets the credential to seal once it passed, nil meanwhile. The tokens stay
/// in that JSON, never on screen.
private struct AWSIdentityCenterSections: View {
    let onReady: (AWSSignedIn?) -> Void

    private enum Step {
        case form, working
        case device(AWSSignInStart)
        case roles(AWSSignInFinish)
        case failed(String)
        case chosen(AWSSignInChoice, LoadState<AWSCheck>)
    }

    @Environment(\.openURL) private var openURL
    @State private var startUrl = ""
    @State private var ssoRegion = ""
    @State private var region = ""
    @State private var step = Step.form
    @State private var task: Task<Void, Never>?

    init(onReady: @escaping (AWSSignedIn?) -> Void) {
        self.onReady = onReady
    }

    var body: some View {
        Section {
            Text(
                "Signs in as aws sso login does: nothing long-lived is stored. The permission set of the role needs the actions of the policy above."
            )
            .font(.footnote).foregroundStyle(.secondary)
            // On this row, always shown: a List gives a Section's modifiers to each of its rows,
            // so the form rows leaving for the code screen would cancel the sign-in just started.
            // The sheet closed or switched to keys: the wait for the browser ends.
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
        case .device(let start):
            deviceRows(start)
        case .roles(let finish):
            Text("Choose an account and role").font(.subheadline.weight(.semibold))
            ForEach(finish.roles, id: \.self) { role in
                Button {
                    task = Task { await choose(finish.credential, role) }
                } label: {
                    Text(verbatim: role.label)
                }
            }
        case .failed(let message):
            Text(verbatim: message).foregroundStyle(.statusBad)
            Button("Retry") { step = .form }
        case .chosen(let choice, let check):
            Text("Signed in with IAM Identity Center as \(choice.arn)").font(.footnote)
            AWSCheckRows(state: check)
            Text(
                "The role's keys are renewed while your Identity Center session lasts (8 hours by default, set by your administrator); sign in again after it ends."
            )
            .font(.footnote).foregroundStyle(.secondary)
        }
    }

    @ViewBuilder private var formRows: some View {
        TextField("IAM Identity Center start URL", text: $startUrl)
            .keyboardType(.URL)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        TextField("IAM Identity Center region", text: $ssoRegion)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        TextField("EC2 region (empty: the Identity Center region)", text: $region)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        Button("Sign in") { signIn() }
            .disabled(trimmed(startUrl).isEmpty || trimmed(ssoRegion).isEmpty)
    }

    @ViewBuilder
    private func deviceRows(_ start: AWSSignInStart) -> some View {
        Text("Enter this code on the sign-in page:")
        Text(verbatim: start.userCode)
            .font(.system(.largeTitle, design: .monospaced).weight(.semibold))
            .textSelection(.enabled)
            .frame(maxWidth: .infinity)
        Button {
            UIPasteboard.general.string = start.userCode
        } label: {
            Label("Copy code", systemImage: "doc.on.doc")
        }
        if let url = URL(string: start.openURL) {
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
        Button("Cancel", role: .cancel) {
            cancel()
            step = .form
        }
    }

    private func trimmed(_ text: String) -> String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func cancel() {
        TalosClient.awsSignInCancel()
        task?.cancel()
    }

    private func signIn() {
        onReady(nil)
        let fields = ["ssoStartUrl": trimmed(startUrl), "ssoRegion": trimmed(ssoRegion), "region": trimmed(region)]
        let request = (try? JSONEncoder().encode(fields)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        step = .working
        task = Task {
            do {
                let start = try await TalosClient.awsSignInStart(request: request)
                step = .device(start)
                let finish = try await TalosClient.awsSignInFinish(credential: start.credential)
                // Cancelled meanwhile: the form is back already.
                guard case .device = step else { return }
                if finish.roles.count == 1, let only = finish.roles.first {
                    await choose(finish.credential, only)
                } else {
                    step = .roles(finish)
                }
            } catch {
                if case .form = step { return }
                step = .failed(error.localizedDescription)
            }
        }
    }

    private func choose(_ credential: String, _ role: KubeSsoRoleChoice) async {
        step = .working
        do {
            let choice = try await TalosClient.awsSignInChoose(credential: credential, role: role)
            let check = await LoadState.from { try await TalosClient.awsTest(credential: choice.credential) }
            if case .loaded(let tested, _, _) = check {
                let sealed = tested.credential.isEmpty ? choice.credential : tested.credential
                onReady(AWSSignedIn(credential: sealed, check: tested))
            }
            step = .chosen(choice, check)
        } catch {
            step = .failed(error.localizedDescription)
        }
    }
}
