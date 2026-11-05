import IchorCore
import SwiftUI
import UIKit

/// "Create the instance at AWS": the run makes the user data itself. On Omni only a name is asked;
/// on plain Talos the card's new member is used. The price, the typed name and the app lock come
/// before the billable create; the run's timeline then replaces the form. Sections of
/// BootMediaView's list.
struct AWSCreateSection: View {
    let omni: Bool
    let template: String
    let role: String
    let hostname: String
    let disk: String
    /// On Omni, the labels to sign into the join token.
    let labels: String
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @State private var accounts: [String] = []
    @State private var account = ""
    @State private var options: LoadState<AWSOptions>?
    @State private var typeName = ""
    @State private var imageId = ""
    @State private var subnetId = ""
    @State private var groupIds: [String] = []
    @State private var publicIp = false
    @State private var price: AWSPrice?
    @State private var omniName = ""
    @State private var confirming = false
    @State private var adding = false
    @State private var makingAMI = false
    @State private var confirmStop = false
    @State private var authError: String?
    private var job: JoinJob { .shared }

    // Explicit: the private @State makes the memberwise init private.
    init(
        omni: Bool, template: String, role: String, hostname: String, disk: String, labels: String,
        path: Binding<[Route]>
    ) {
        _path = path
        self.omni = omni
        self.template = template
        self.role = role
        self.hostname = hostname
        self.disk = disk
        self.labels = labels
    }

    private var credential: String {
        account.isEmpty ? "" : CloudProviderStore.token(provider: CloudProviderStore.aws, project: account)
    }

    private var demo: Bool { model.activeSummary?.demo == true }
    private var name: String { omni ? omniName.trimmingCharacters(in: .whitespaces) : hostname }

    var body: some View {
        if job.request?.aws != nil {
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
                Text("Create the instance at AWS")
            }
            .task { await load() }
            .sheet(isPresented: $adding) {
                AWSKeysSheet { added in
                    reloadAccounts()
                    pick(added)
                }
            }
            if job.request?.awsImage != nil {
                ImageRunSections { outcome in
                    guard case .amiMade(let made)? = outcome else { return }
                    Task { await amiClosed(made) }
                }
            }
            if let options { optionsSections(options) }
        }
    }

    @ViewBuilder private var accountRows: some View {
        if !accounts.isEmpty {
            Picker("Account", selection: Binding(get: { account }, set: { pick($0) })) {
                ForEach(accounts, id: \.self) { Text(verbatim: $0).tag($0) }
            }
        }
        Button("Add AWS access keys") { adding = true }
        if !account.isEmpty {
            Button("Remove these keys", role: .destructive) {
                try? CloudProviderStore.save(provider: CloudProviderStore.aws, project: account, token: "")
                reloadAccounts()
                pick(accounts.first ?? "")
            }
        }
    }

    @ViewBuilder
    private func optionsSections(_ state: LoadState<AWSOptions>) -> some View {
        switch state {
        case .loading:
            Section { ProgressView().frame(maxWidth: .infinity) }
        case .failed(let error):
            Section {
                Text(verbatim: error).foregroundStyle(.statusBad)
                Button("Retry") { Task { await loadOptions() } }
            }
        case .loaded(let value, _, _):
            placeSection(value)
            machineSection(value)
            networkSection(value)
            createSection(value)
        }
    }

    /// The host name on Omni, the region and the core's warnings.
    @ViewBuilder
    private func placeSection(_ value: AWSOptions) -> some View {
        Section {
            if omni {
                TextField("Host name", text: $omniName)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            if !demo, value.regions.count > 1 {
                Picker("Region", selection: Binding(get: { value.region }, set: { switchRegion($0) })) {
                    ForEach(value.regions, id: \.self) { Text(verbatim: $0).tag($0) }
                }
            } else {
                LabeledContent("Region") { Text(verbatim: value.region) }
            }
            ForEach(value.warnings, id: \.self) { warning in
                Text(verbatim: warning).font(.footnote).foregroundStyle(.orange)
            }
        }
    }

    /// The instance type, grouped by arch, and the AMIs that arch boots.
    @ViewBuilder
    private func machineSection(_ value: AWSOptions) -> some View {
        let type = value.types.first { $0.name == typeName }
        Section {
            Picker("Instance type", selection: $typeName) {
                Text(verbatim: "—").tag("")
                ForEach(value.arches, id: \.self) { arch in
                    Section(arch) {
                        ForEach(value.types(for: arch)) { option in
                            typeLabel(option).tag(option.name)
                        }
                    }
                }
            }
            .onChange(of: typeName) { old, new in
                // Another arch: the picked AMI cannot boot it.
                let arch = { (name: String) in value.types.first { $0.name == name }?.arch }
                if arch(old) != arch(new) { imageId = "" }
            }
            if let type {
                let images = value.images(for: type.arch)
                if images.isEmpty, demo {
                    Text("No Talos AMI for \(type.arch) here: pick another region, or an AMI of your own.")
                        .font(.footnote).foregroundStyle(.orange)
                }
                if !images.isEmpty {
                    Picker("Talos AMI", selection: $imageId) {
                        Text(verbatim: "—").tag("")
                        ForEach(images) { imageLabel($0, options: value).tag($0.id) }
                    }
                }
                if !demo { makeAMIRows(type: type, options: value) }
            }
        }
        .task(id: typeName) { await loadPrice() }
        .sheet(isPresented: $makingAMI) {
            AWSImageSheet(
                options: value, arch: type?.arch ?? "", subnetId: subnetId, groupId: groupIds.first ?? "",
                template: omni ? "" : template, credential: credential
            ) { spec in
                startAMI(spec)
            }
        }
    }

    /// The AMI's name, marked when it is the cluster's image.
    private func imageLabel(_ image: AWSImage, options: AWSOptions) -> Text {
        let name = image.name.isEmpty ? image.id : image.name
        return image.matches(options) ? Text("\(name) · matches the cluster's image") : Text(verbatim: name)
    }

    /// An AMI of another version or schematic boots Talos without the cluster's extensions: offer one of its image.
    @ViewBuilder
    private func makeAMIRows(type: AWSInstanceType, options: AWSOptions) -> some View {
        let region = options.region
        if options.hasClusterImage(for: type.arch) {
            Button("Make another Talos AMI") { makingAMI = true }
                .disabled(job.isActive || account.isEmpty)
        } else {
            Text("No Talos AMI of this cluster's image for \(type.arch) in \(region) yet: Ichor can make one.")
                .font(.footnote).foregroundStyle(.secondary)
            Button("Make a Talos AMI") { makingAMI = true }
                .disabled(job.isActive || account.isEmpty)
        }
    }

    /// The subnet, the security groups of its VPC and the public address.
    @ViewBuilder
    private func networkSection(_ value: AWSOptions) -> some View {
        let subnet = value.subnets.first { $0.id == subnetId }
        Section {
            Picker("Subnet", selection: $subnetId) {
                Text(verbatim: "—").tag("")
                ForEach(value.subnets) { subnetLabel($0).tag($0.id) }
            }
            .onChange(of: subnetId) { old, new in
                // Groups of another VPC are refused with this subnet.
                let vpc = { (id: String) in value.subnets.first { $0.id == id }?.vpcId }
                if vpc(old) != vpc(new) { groupIds = [] }
                publicIp = value.subnets.first { $0.id == new }?.public ?? false
            }
            if let subnet {
                ForEach(value.securityGroups(in: subnet.vpcId)) { group in
                    Toggle(
                        isOn: Binding(
                            get: { groupIds.contains(group.id) },
                            set: { on in
                                groupIds.removeAll { $0 == group.id }
                                if on { groupIds.append(group.id) }
                            }
                        )
                    ) {
                        Text(verbatim: group.name.isEmpty ? group.id : group.name)
                    }
                }
                Toggle("Give it a public IP address", isOn: $publicIp)
            }
        } header: {
            Text("Subnet")
        } footer: {
            if subnet != nil, groupIds.isEmpty {
                Text("None picked: the VPC's default group applies.")
            }
        }
    }

    /// The price line, then the confirmed Create.
    @ViewBuilder
    private func createSection(_ value: AWSOptions) -> some View {
        let type = value.types.first { $0.name == typeName }
        let image = type.flatMap { t in value.images(for: t.arch).first { $0.id == imageId } }
        let subnet = value.subnets.first { $0.id == subnetId }
        Section {
            if type != nil {
                if let price {
                    Group {
                        if price.hourly.isEmpty {
                            Text("Price unavailable: see AWS's EC2 pricing.")
                        } else {
                            Text("≈ $\(price.hourly)/h, before tax")
                        }
                    }
                    .font(.headline).foregroundStyle(.orange)
                } else {
                    ProgressView().frame(maxWidth: .infinity)
                }
            }
            if let authError {
                Text(verbatim: authError).foregroundStyle(.statusBad)
            }
            Button("Create the server", role: .destructive) { confirming = true }
                .disabled(
                    name.isEmpty || (!omni && (template.isEmpty || disk.isEmpty)) || type == nil || image == nil
                        || subnet == nil || job.isActive
                )
                .sheet(isPresented: $confirming) {
                    if let type, let image, let subnet {
                        HostnameConfirmationSheet(
                            title: String(localized: "Create \(name) at AWS?"),
                            message: String(
                                localized:
                                    "This creates a billable instance (\(type.name) in \(value.region)), billed until it is terminated. Its user data goes to AWS with the cluster's secrets or Omni's join token. A failed run terminates the instance."
                            ),
                            hostname: name,
                            actionTitle: String(localized: "Create the server")
                        ) {
                            confirming = false
                            let groups = value.securityGroups(in: subnet.vpcId).map(\.id)
                                .filter { groupIds.contains($0) }
                            let spec = AWSCreateSpec(
                                region: value.region, instanceType: type.name, imageId: image.id, subnetId: subnet.id,
                                securityGroupIds: groups, associatePublicIp: publicIp, hostname: name,
                                role: omni ? "" : role, templateNode: omni ? "" : template,
                                installDisk: omni ? "" : disk, labelsJson: omni ? labels : "")
                            Task { await create(spec) }
                        }
                    }
                }
        }
    }

    private func typeLabel(_ type: AWSInstanceType) -> Text {
        let base = "\(type.name) · \(type.vcpus) CPU · \(type.memoryLabel) GB"
        guard type.burstable else { return Text(verbatim: base) }
        return Text(verbatim: base + " · " + String(localized: "burstable"))
    }

    private func subnetLabel(_ subnet: AWSSubnet) -> Text {
        let base = [subnet.name.isEmpty ? subnet.id : subnet.name, subnet.az, subnet.cidr]
            .filter { !$0.isEmpty }.joined(separator: " · ")
        guard subnet.public else { return Text(verbatim: base) }
        return Text(verbatim: base + " · " + String(localized: "assigns a public IP"))
    }

    @ViewBuilder private var runSections: some View {
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(label: step.phase.localizedLabel, state: step.state, at: step.at, message: step.message)
            }
        } header: {
            Text("Create the instance at AWS")
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
            case .snapshotMade?, .amiMade?:
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
                "\(job.request?.hostname ?? "") is terminated at AWS, which ends its billing. A control plane that already answered is kept: its etcd member must go first."
            )
        }
    }

    private func reloadAccounts() {
        accounts = CloudProviderStore.projects(provider: CloudProviderStore.aws)
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
        typeName = ""
        imageId = ""
        subnetId = ""
        groupIds = []
        price = nil
        if newAccount.isEmpty {
            options = nil
        } else {
            Task { await loadOptions() }
        }
    }

    /// The same keys in another region: re-sealed with it, then the region's options.
    private func switchRegion(_ region: String) {
        guard !account.isEmpty,
            let keys = try? JSONDecoder().decode(AWSCredential.self, from: Data(credential.utf8)),
            keys.region != region,
            let json = try? JSONEncoder().encode(keys.with(region: region))
        else { return }
        do {
            try CloudProviderStore.save(
                provider: CloudProviderStore.aws, project: account, token: String(decoding: json, as: UTF8.self))
        } catch {
            authError = error.localizedDescription
            return
        }
        pick(account)
    }

    private func loadOptions() async {
        guard let client = model.client else { return }
        let credential = self.credential
        options = .loading
        options = await LoadState.from { try await client.awsOptions(credential: credential) }
    }

    /// The Price List's answer for the picked type; a failure (the demo has no keys) is "price unavailable".
    private func loadPrice() async {
        price = nil
        guard !typeName.isEmpty else { return }
        let credential = self.credential
        let type = typeName
        let answer = (try? await TalosClient.awsPrice(credential: credential, instanceType: type)) ?? AWSPrice()
        if !Task.isCancelled { price = answer }
    }

    /// The throwaway instance's run: followed like a join, with no node to add at its end.
    private func startAMI(_ spec: AWSImageSpec) {
        guard let client = model.client else { return }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: "", hostname: "Talos \(spec.arch)", disk: "",
            staticAddress: "", awsImage: spec)
        job.start(client: client, request: request, providerToken: credential) { _ in }
    }

    /// The region's AMIs again, with the new one picked.
    private func amiClosed(_ made: AWSImageResult) async {
        await loadOptions()
        guard case .loaded(let value, _, _) = options, value.images.contains(where: { $0.id == made.imageId }) else {
            return
        }
        imageId = made.imageId
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func create(_ spec: AWSCreateSpec) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name) at AWS?"))
        {
            authError = failure
            return
        }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: spec.role, hostname: name, disk: spec.installDisk,
            staticAddress: "", aws: spec, omni: omni)
        // The cluster the node joins, even if another one is active when it has joined.
        let context = model.activeContext
        job.start(client: client, request: request, providerToken: credential) { address in
            try await model.addNodes([address], to: context)
        }
    }
}

/// The access keys (masked), an optional session token, the region, the policy to give them, a
/// test, then sealed under the account id.
private struct AWSKeysSheet: View {
    let onSaved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var accessKeyId = ""
    @State private var secret = ""
    @State private var sessionToken = ""
    @State private var region = ""
    @State private var check: LoadState<AWSCheck>?
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
                        "Access keys do not expire unless you rotate them and can do what their IAM policy allows. They stay sealed on this device and are not in backups."
                    )
                    .font(.footnote).foregroundStyle(.orange)
                    Text(
                        "Give the keys' IAM user this policy: only the calls Ichor makes, and it may terminate only the instances Ichor tagged."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Button("Copy the IAM policy") {
                        UIPasteboard.general.string = TalosClient.awsPolicy()
                    }
                }
                Section {
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
                    Section { result(check) }
                }
                if let saveError {
                    Section { Text(verbatim: saveError).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add AWS access keys")
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
    private func result(_ state: LoadState<AWSCheck>) -> some View {
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

    private var tested: AWSCheck? {
        guard case .loaded(let value, _, _) = check else { return nil }
        return value
    }

    private var canSave: Bool {
        guard let tested, tested.canCreate || !tested.checked else { return false }
        return !tested.account.isEmpty
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
        guard let account = tested?.account else { return }
        do {
            try CloudProviderStore.save(provider: CloudProviderStore.aws, project: account, token: credentialJSON())
            onSaved(account)
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}
