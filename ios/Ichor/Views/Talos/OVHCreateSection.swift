import IchorCore
import SwiftUI
import UIKit

/// "Create the instance at OVHcloud": the run makes the user data itself. On Omni only a name is
/// asked; on plain Talos the card's new member is used. The cost, the typed name and the app lock
/// come before the billable create; the run's timeline then replaces the form. Sections of
/// BootMediaView's list.
struct OVHCreateSection: View {
    let omni: Bool
    let template: String
    let role: String
    let hostname: String
    let disk: String
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @State private var accounts: [String] = []
    @State private var account = ""
    @State private var options: LoadState<OVHOptions>?
    @State private var region = ""
    @State private var flavor: OVHFlavor?
    @State private var image: OVHImage?
    @State private var omniName = ""
    @State private var confirming = false
    @State private var adding = false
    @State private var confirmStop = false
    @State private var authError: String?
    private var job: JoinJob { .shared }

    // Explicit: the private @State makes the memberwise init private.
    init(omni: Bool, template: String, role: String, hostname: String, disk: String, path: Binding<[Route]>) {
        _path = path
        self.omni = omni
        self.template = template
        self.role = role
        self.hostname = hostname
        self.disk = disk
    }

    private var credential: String {
        account.isEmpty ? "" : CloudProviderStore.token(provider: CloudProviderStore.ovh, project: account)
    }

    private var demo: Bool { model.activeSummary?.demo == true }
    private var name: String { omni ? omniName.trimmingCharacters(in: .whitespaces) : hostname }

    var body: some View {
        if job.request?.ovh != nil {
            runSections
        } else {
            Section {
                if demo {
                    Text("Demo cluster: these are samples, and nothing is created.")
                        .font(.footnote).foregroundStyle(.secondary)
                } else {
                    accountRows
                }
            } header: {
                Text("Create the instance at OVHcloud")
            }
            .task { await load() }
            .sheet(isPresented: $adding) {
                OVHAccountSheet { added in
                    reloadAccounts()
                    pick(added)
                }
            }
            if let options { optionsSection(options) }
        }
    }

    @ViewBuilder private var accountRows: some View {
        if !accounts.isEmpty {
            Picker("Project", selection: Binding(get: { account }, set: { pick($0) })) {
                ForEach(accounts, id: \.self) { Text(verbatim: $0).tag($0) }
            }
        }
        Button("Add an OVHcloud service account") { adding = true }
        if !account.isEmpty {
            Button("Remove this account", role: .destructive) {
                try? CloudProviderStore.save(provider: CloudProviderStore.ovh, project: account, token: "")
                reloadAccounts()
                pick(accounts.first ?? "")
            }
        }
    }

    @ViewBuilder
    private func optionsSection(_ state: LoadState<OVHOptions>) -> some View {
        Section {
            switch state {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let error):
                Text(verbatim: error).foregroundStyle(.statusBad)
                Button("Retry") { Task { await loadOptions() } }
            case .loaded(let value, _, _):
                instanceForm(value)
            }
        }
    }

    @ViewBuilder
    private func instanceForm(_ value: OVHOptions) -> some View {
        if omni {
            TextField("Host name", text: $omniName)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
        Picker("Region", selection: $region) {
            Text(verbatim: "—").tag("")
            ForEach(value.regions, id: \.self) { Text(verbatim: $0).tag($0) }
        }
        let flavors = value.flavors(in: region)
        let images = value.images(in: region)
        Picker("Flavor", selection: $flavor) {
            Text(verbatim: "—").tag(OVHFlavor?.none)
            ForEach(flavors) { option in
                Text(verbatim: "\(option.name) · \(option.vcpus) CPU · \(option.ramGB) GB")
                    .tag(OVHFlavor?.some(option))
            }
        }
        if !region.isEmpty, !images.contains(where: \.talos) {
            Text(
                "No Talos image in \(region): upload the Talos cloud image for OpenStack to this project once from a computer."
            )
            .font(.footnote).foregroundStyle(.secondary)
        }
        Picker("Talos image", selection: $image) {
            Text(verbatim: "—").tag(OVHImage?.none)
            ForEach(images) { Text(verbatim: $0.name).tag(OVHImage?.some($0)) }
        }
        let chosenFlavor = flavors.first { $0.id == flavor?.id }
        let chosenImage = images.first { $0.id == image?.id }
        if let chosenFlavor {
            Group {
                if chosenFlavor.hourly.isEmpty {
                    Text("Price unavailable: see OVHcloud's price list.")
                } else {
                    let hourly = "\(chosenFlavor.hourly) \(value.currency)"
                    Text("About \(hourly) per hour, before tax")
                }
            }
            .font(.headline).foregroundStyle(.orange)
        }
        if let authError {
            Text(verbatim: authError).foregroundStyle(.statusBad)
        }
        Button("Create the server", role: .destructive) { confirming = true }
            .disabled(
                name.isEmpty || (!omni && (template.isEmpty || disk.isEmpty)) || chosenFlavor == nil
                    || chosenImage == nil || job.isActive
            )
            .sheet(isPresented: $confirming) {
                if let chosenFlavor, let chosenImage {
                    HostnameConfirmationSheet(
                        title: String(localized: "Create \(name) at OVHcloud?"),
                        message: String(
                            localized:
                                "This creates a billable instance (\(chosenFlavor.name) in \(region)). Its user data goes to OVHcloud with the cluster's secrets or Omni's join token. A failed run deletes the instance."
                        ),
                        hostname: name,
                        actionTitle: String(localized: "Create the server")
                    ) {
                        confirming = false
                        Task { await create(flavor: chosenFlavor, image: chosenImage) }
                    }
                }
            }
    }

    @ViewBuilder private var runSections: some View {
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(label: step.phase.localizedLabel, state: step.state, at: step.at, message: step.message)
            }
        } header: {
            Text("Create the instance at OVHcloud")
        }
        Section {
            switch job.outcome {
            case .joined(let address)?:
                Text("\(job.request?.hostname ?? "") joined the cluster as \(address)")
                Button("Close") { job.clear() }
            case .appearedInOmni?:
                Text("\(job.request?.hostname ?? "") appeared in Omni: allocate it there")
                Button("Allocate in Omni") {
                    job.clear()
                    path.append(.omniAllocate)
                }
                Button("Close") { job.clear() }
            case .snapshotMade?:
                Button("Close") { job.clear() }
            case .failed(let error)?:
                Text(verbatim: error).foregroundStyle(.statusBad)
                Button("Close") { job.clear() }
            case nil:
                Button(
                    job.stopping ? String(localized: "Stopping…") : String(localized: "Stop and delete the server"),
                    role: .destructive
                ) {
                    confirmStop = true
                }
                .disabled(job.stopping)
            }
        } footer: {
            if job.isActive {
                Text("Stopping deletes the server this run created.")
            }
        }
        .alert(Text("Stop and delete the server"), isPresented: $confirmStop) {
            Button("Stop and delete the server", role: .destructive) { job.requestStop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(
                "\(job.request?.hostname ?? "") is deleted at OVHcloud. A control plane that already answered is kept: its etcd member must go first."
            )
        }
    }

    private func reloadAccounts() {
        accounts = CloudProviderStore.projects(provider: CloudProviderStore.ovh)
    }

    private func load() async {
        reloadAccounts()
        if demo {
            await loadOptions()
        } else if !accounts.contains(account) {
            pick(accounts.first ?? "")
        }
    }

    private func pick(_ newAccount: String) {
        account = newAccount
        region = ""
        flavor = nil
        image = nil
        if newAccount.isEmpty {
            options = nil
        } else {
            Task { await loadOptions() }
        }
    }

    private func loadOptions() async {
        guard let client = model.client else { return }
        let credential = self.credential
        options = .loading
        options = await LoadState.from { try await client.ovhOptions(credential: credential) }
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func create(flavor: OVHFlavor, image: OVHImage) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name) at OVHcloud?"))
        {
            authError = failure
            return
        }
        let spec = OVHCreateSpec(
            region: region, flavorId: flavor.id, imageId: image.id, hostname: name,
            role: omni ? "" : role, templateNode: omni ? "" : template, installDisk: omni ? "" : disk)
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: spec.role, hostname: name, disk: spec.installDisk,
            staticAddress: "", ovh: spec, omni: omni)
        // The cluster the node joins, even if another one is active when it has joined.
        let context = model.activeContext
        job.start(client: client, request: request, providerToken: credential) { address in
            try await model.addNodes([address], to: context)
        }
    }
}

/// A label, the service account (masked), the policy to give it, a test, then sealed.
private struct OVHAccountSheet: View {
    let onSaved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var label = ""
    @State private var endpoint = "eu"
    @State private var clientId = ""
    @State private var secret = ""
    @State private var project = ""
    @State private var check: LoadState<OVHCheck>?
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
                        "A service account's secret does not expire and can do what its IAM policy allows. It stays sealed on this device and is not in backups."
                    )
                    .font(.footnote).foregroundStyle(.orange)
                    Text(
                        "Give the service account this IAM policy at OVHcloud: only the calls Ichor makes, on one project."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Button("Copy the IAM policy") {
                        UIPasteboard.general.string = TalosClient.ovhPolicy(project: project)
                    }
                }
                Section {
                    TextField("Project name (yours, for this list)", text: $label)
                    Picker("API region", selection: $endpoint) {
                        Text(verbatim: "EU").tag("eu")
                        Text(verbatim: "CA").tag("ca")
                    }
                    .onChange(of: endpoint) { check = nil }
                    TextField("Client ID", text: $clientId)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: clientId) { check = nil }
                    SecureField("Client secret", text: $secret)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: secret) { check = nil }
                    Button("Test") { Task { await test() } }
                        .disabled(clientId.isEmpty || secret.isEmpty)
                }
                if let check {
                    Section { result(check) }
                }
                if let saveError {
                    Section { Text(verbatim: saveError).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add an OVHcloud service account")
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
    private func result(_ state: LoadState<OVHCheck>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
        case .loaded(let value, _, _):
            if value.projects.count > 1 {
                Picker("Project", selection: $project) {
                    ForEach(value.projects, id: \.self) { Text(verbatim: $0).tag($0) }
                }
            }
            if !value.checked {
                Text("Signed in to project \(project). Whether it may create instances shows when it tries.")
            } else if value.canCreate {
                Text("It may create instances in project \(project).").foregroundStyle(.statusOK)
            } else {
                Text("It may not create instances: add the policy's actions.").foregroundStyle(.orange)
            }
        }
    }

    private var canSave: Bool {
        guard case .loaded(let value, _, _) = check, value.canCreate || !value.checked else { return false }
        return !label.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private func credentialJSON() -> String {
        let credential = OVHCredential(
            endpoint: endpoint, clientId: clientId.trimmingCharacters(in: .whitespacesAndNewlines),
            clientSecret: secret.trimmingCharacters(in: .whitespacesAndNewlines), project: project)
        return (try? JSONEncoder().encode(credential)).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }

    private func test() async {
        let credential = credentialJSON()
        check = .loading
        let state = await LoadState.from { try await TalosClient.ovhTest(credential: credential) }
        if case .loaded(let value, _, _) = state { project = value.project }
        check = state
    }

    private func save() {
        let name = label.trimmingCharacters(in: .whitespaces)
        do {
            try CloudProviderStore.save(provider: CloudProviderStore.ovh, project: name, token: credentialJSON())
            onSaved(name)
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}
