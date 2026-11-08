import IchorCore
import SwiftUI
import UIKit

/// The Azure clouds a principal can belong to; `environment` is the core's name ("" = public).
enum AzureCloud: String, CaseIterable, Identifiable {
    case `public` = ""
    case china = "AzureChinaCloud"
    case usGovernment = "AzureUSGovernmentCloud"
    var id: String { rawValue }

    var label: String {
        switch self {
        case .public: "Azure"
        case .china: "Azure China"
        case .usGovernment: "Azure US Government"
        }
    }
}

/// A credential JSON to seal, and the test it passed.
struct AzureSignedIn {
    let credential: String
    let check: AzureCheck
}

/// The principal's ids, its secret (masked), its cloud and resource group, or a Microsoft-account
/// sign-in; the role to give it, a test, then sealed under the subscription id.
struct AzureAccountSheet: View {
    let onSaved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var microsoftAccount = false
    @State private var tenantId = ""
    @State private var clientId = ""
    @State private var secret = ""
    @State private var subscriptionId = ""
    @State private var resourceGroup = ""
    @State private var cloud = AzureCloud.public
    @State private var check: LoadState<AzureCheck>?
    @State private var signedIn: AzureSignedIn?
    @State private var saveError: String?

    // Explicit: the private @State makes the memberwise init private.
    init(onSaved: @escaping (String) -> Void) {
        self.onSaved = onSaved
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Account", selection: $microsoftAccount) {
                        Text("Service principal").tag(false)
                        Text("Microsoft account").tag(true)
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .onChange(of: microsoftAccount) { signedIn = nil }
                    Text(
                        "Create this custom role (az role definition create) and assign it to the principal on the resource group: only the calls Ichor makes."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Button("Copy the role definition") {
                        UIPasteboard.general.string = TalosClient.azureRole(
                            subscription: trim(subscriptionId), resourceGroup: trim(resourceGroup))
                    }
                }
                if microsoftAccount {
                    AzureMicrosoftAccountSections { signedIn = $0 }
                } else {
                    principalSections
                }
                if let saveError {
                    Section { Text(verbatim: saveError).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add an Azure account")
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

    @ViewBuilder private var principalSections: some View {
        Section {
            Text(
                "A client secret expires on the date set when it was made and can do what its role allows. It stays sealed on this device and is not in backups."
            )
            .font(.footnote).foregroundStyle(.orange)
            Picker("Cloud", selection: $cloud) {
                ForEach(AzureCloud.allCases) { Text(verbatim: $0.label).tag($0) }
            }
            .onChange(of: cloud) { check = nil }
            field("Tenant ID", text: $tenantId)
            field("Client ID (appId)", text: $clientId)
            SecureField("Client secret (password)", text: $secret)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onChange(of: secret) { check = nil }
            field("Subscription ID", text: $subscriptionId)
            field("Resource group", text: $resourceGroup)
            Button("Test") { Task { await test() } }
                .disabled(tenantId.isEmpty || clientId.isEmpty || secret.isEmpty || subscriptionId.isEmpty)
        } footer: {
            Text("The resource group the role is assigned on: Test checks the principal's rights there.")
        }
        if let check {
            Section { AzureCheckRows(state: check, resourceGroup: trim(resourceGroup)) }
        }
    }

    private func field(_ title: LocalizedStringKey, text: Binding<String>) -> some View {
        TextField(title, text: text)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .onChange(of: text.wrappedValue) { check = nil }
    }

    /// What the test passed: the principal, or the sign-in (renewed by the core when it says so).
    private var ready: AzureSignedIn? {
        let passed: AzureSignedIn?
        if microsoftAccount {
            passed = signedIn
        } else if case .loaded(let value, _, _) = check {
            passed = AzureSignedIn(credential: credentialJSON(), check: value)
        } else {
            passed = nil
        }
        guard let passed, passed.check.canCreate || !passed.check.checked, !passed.check.subscription.isEmpty else {
            return nil
        }
        return passed
    }

    private func trim(_ text: String) -> String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func credentialJSON() -> String {
        let principal = AzureCredential(
            tenantId: trim(tenantId), clientId: trim(clientId), clientSecret: trim(secret),
            subscriptionId: trim(subscriptionId), environment: cloud.rawValue, resourceGroup: trim(resourceGroup))
        return (try? JSONEncoder().encode(principal)).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }

    private func test() async {
        let credential = credentialJSON()
        check = .loading
        check = await LoadState.from { try await TalosClient.azureTest(credential: credential) }
    }

    private func save() {
        guard let ready else { return }
        do {
            try CloudProviderStore.save(
                provider: CloudProviderStore.azure, project: ready.check.subscription, token: ready.credential)
            onSaved(ready.check.subscription)
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}

/// What a test of the principal or the sign-in says: which subscription, and whether it may create
/// VMs in `resourceGroup` ("" = the subscription).
struct AzureCheckRows: View {
    let state: LoadState<AzureCheck>
    let resourceGroup: String

    var body: some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
        case .loaded(let value, _, _):
            let name = value.subscriptionName.isEmpty ? value.subscription : value.subscriptionName
            let place = resourceGroup.isEmpty ? name : resourceGroup
            if !value.checked {
                Text("Principal of subscription \(name). Whether it may create VMs in \(place) shows when it tries.")
            } else if value.canCreate {
                Text("The principal of subscription \(name) may create VMs in \(place).")
                    .foregroundStyle(.statusOK)
            } else {
                Text(
                    "The principal of subscription \(name) may not create VMs in \(place): assign it the role (Microsoft.Compute/virtualMachines/write)."
                )
                .foregroundStyle(.orange)
            }
        }
    }
}

/// The Microsoft-account side of the account sheet, as `az login --use-device-code`: an optional
/// tenant and client id, the code to enter in any browser, the subscription to take, then the same
/// test as a principal. `onReady` gets the credential to seal once it passed, nil meanwhile. The
/// refresh token stays in that JSON, never on screen.
private struct AzureMicrosoftAccountSections: View {
    let onReady: (AzureSignedIn?) -> Void

    private enum Step {
        case form, working
        case device(AzureSignInStart)
        case subscriptions(AzureSignInFinish)
        case failed(String)
        case chosen(AzureSignInChoice, LoadState<AzureCheck>)
    }

    @Environment(\.openURL) private var openURL
    @State private var tenant = ""
    @State private var clientId = ""
    @State private var cloud = AzureCloud.public
    @State private var step = Step.form
    @State private var task: Task<Void, Never>?

    init(onReady: @escaping (AzureSignedIn?) -> Void) {
        self.onReady = onReady
    }

    var body: some View {
        Section {
            Text(
                "Signs in as az login --use-device-code does, with Azure CLI's public client unless you enter your own. Assign the role above to your account or one of its groups on the resource group."
            )
            .font(.footnote).foregroundStyle(.secondary)
            stepRows
        }
        // The sheet closed or switched to a principal: the wait for the browser ends.
        .onDisappear { cancel() }
    }

    @ViewBuilder private var stepRows: some View {
        switch step {
        case .form:
            formRows
        case .working:
            ProgressView().frame(maxWidth: .infinity)
        case .device(let start):
            deviceRows(start)
        case .subscriptions(let finish):
            Text("Subscription").font(.subheadline.weight(.semibold))
            ForEach(finish.subscriptions, id: \.self) { subscription in
                Button {
                    task = Task { await choose(finish.credential, subscription.id) }
                } label: {
                    Text(verbatim: subscription.label)
                }
            }
        case .failed(let message):
            Text(verbatim: message).foregroundStyle(.statusBad)
            Button("Retry") { step = .form }
        case .chosen(let choice, let check):
            Text("Signed in with a Microsoft account as \(choice.account)").font(.footnote)
            AzureCheckRows(state: check, resourceGroup: "")
            Text(
                "The sign-in stays sealed on this device and is not in backups. It lasts while you use it (90 days of inactivity by default, set by your administrator); sign in again after it ends."
            )
            .font(.footnote).foregroundStyle(.secondary)
        }
    }

    @ViewBuilder private var formRows: some View {
        Picker("Cloud", selection: $cloud) {
            ForEach(AzureCloud.allCases) { Text(verbatim: $0.label).tag($0) }
        }
        TextField("Tenant id or domain (empty: any work or school account)", text: $tenant)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        TextField("Client id of a public app registration (empty: Azure CLI's)", text: $clientId)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        Button("Sign in") { signIn() }
    }

    @ViewBuilder
    private func deviceRows(_ start: AzureSignInStart) -> some View {
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
        if let url = URL(string: start.verificationUri) {
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
        TalosClient.azureSignInCancel()
        task?.cancel()
    }

    private func signIn() {
        onReady(nil)
        let fields = ["tenantId": trimmed(tenant), "clientId": trimmed(clientId), "environment": cloud.rawValue]
        let request = (try? JSONEncoder().encode(fields)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        step = .working
        task = Task {
            do {
                let start = try await TalosClient.azureSignInStart(request: request)
                step = .device(start)
                let finish = try await TalosClient.azureSignInFinish(credential: start.credential)
                // Cancelled meanwhile: the form is back already.
                guard case .device = step else { return }
                if finish.subscriptions.count == 1, let only = finish.subscriptions.first {
                    await choose(finish.credential, only.id)
                } else {
                    step = .subscriptions(finish)
                }
            } catch {
                if case .form = step { return }
                step = .failed(error.localizedDescription)
            }
        }
    }

    private func choose(_ credential: String, _ subscription: String) async {
        step = .working
        do {
            let choice = try await TalosClient.azureSignInChoose(credential: credential, subscription: subscription)
            let check = await LoadState.from { try await TalosClient.azureTest(credential: choice.credential) }
            if case .loaded(let tested, _, _) = check {
                let sealed = tested.credential.isEmpty ? choice.credential : tested.credential
                onReady(AzureSignedIn(credential: sealed, check: tested))
            }
            step = .chosen(choice, check)
        } catch {
            step = .failed(error.localizedDescription)
        }
    }
}
