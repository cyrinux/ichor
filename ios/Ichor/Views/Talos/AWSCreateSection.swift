import IchorCore
import SwiftUI

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
    /// The guided network's group, picked once its subnet is (the subnet's change clears the groups).
    @State private var pendingGroup: String?
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

    /// The picked account's IAM Identity Center sign-in (no secret read from it), nil for access keys.
    private var signedIn: AWSCredential? {
        let decoded = try? JSONDecoder().decode(AWSCredential.self, from: Data(credential.utf8))
        guard let stored = decoded else { return nil }
        return stored.identityCenter ? stored : nil
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
                AWSAccountSheet { added in
                    reloadAccounts()
                    pick(added)
                }
            }
            if job.request?.awsImage != nil {
                ImageRunSections { outcome in
                    guard case .imageMade(let made)? = outcome else { return }
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
        if let signedIn {
            Text("Signed in with IAM Identity Center as \(signedIn.role.label)")
                .font(.footnote).foregroundStyle(.secondary)
        }
        Button("Add an AWS account") { adding = true }
        if !account.isEmpty {
            if signedIn != nil {
                Button("Sign out", role: .destructive) { removeAccount() }
            } else {
                Button("Remove these keys", role: .destructive) { removeAccount() }
            }
        }
    }

    private func removeAccount() {
        try? CloudProviderStore.save(provider: CloudProviderStore.aws, project: account, token: "")
        reloadAccounts()
        pick(accounts.first ?? "")
    }

    /// Seals again what the core renewed (an Identity Center sign-in's tokens); "" is nothing to do.
    private func reseal(_ updated: String) {
        guard !account.isEmpty, !updated.isEmpty else { return }
        try? CloudProviderStore.save(provider: CloudProviderStore.aws, project: account, token: updated)
    }

    /// The credential for a run, which uses only what it is given: an Identity Center sign-in is
    /// renewed first. A renewal that fails passes the stored one, and the run says why.
    private func runCredential() async -> String {
        let stored = credential
        guard !account.isEmpty else { return stored }
        let renewed = (try? await TalosClient.awsRenew(credential: stored))?.credential ?? ""
        reseal(renewed)
        return renewed.isEmpty ? stored : renewed
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
            if !demo, !account.isEmpty {
                AWSGuidedNetworkRows(
                    region: value.region, omni: omni, role: omni ? "" : role, credential: credential,
                    disabled: job.isActive
                ) { network in
                    Task { await guidedNetworkReady(network) }
                }
            }
            Picker("Subnet", selection: $subnetId) {
                Text(verbatim: "—").tag("")
                ForEach(value.subnets) { subnetLabel($0).tag($0.id) }
            }
            .onChange(of: subnetId) { old, new in
                // Groups of another VPC are refused with this subnet.
                let vpc = { (id: String) in value.subnets.first { $0.id == id }?.vpcId }
                if vpc(old) != vpc(new) { groupIds = [] }
                publicIp = value.subnets.first { $0.id == new }?.public ?? false
                if let pendingGroup {
                    // The guided network: its group, and an address even where the subnet gives none.
                    groupIds = [pendingGroup]
                    publicIp = true
                    self.pendingGroup = nil
                }
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
                    if group.wideOpen, groupIds.contains(group.id) {
                        Text("This group is open to the world on every port.")
                            .font(.footnote).foregroundStyle(.orange)
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
            case .snapshotMade?, .imageMade?:
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

    /// The same keys in another region: re-sealed with it, then the region's options. Edited as
    /// JSON: an Identity Center sign-in holds tokens AWSCredential does not keep.
    private func switchRegion(_ region: String) {
        guard !account.isEmpty,
            let keys = try? JSONDecoder().decode(AWSCredential.self, from: Data(credential.utf8)),
            keys.region != region,
            let json = AWSCredential.moved(credential, to: region)
        else { return }
        do {
            try CloudProviderStore.save(provider: CloudProviderStore.aws, project: account, token: json)
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
        if case .loaded(let value, _, _) = options { reseal(value.credential) }
    }

    /// The guided network is ready: its group is listed after a refresh that keeps the form on
    /// screen, then its subnet and group are picked.
    private func guidedNetworkReady(_ network: AWSNetwork) async {
        reseal(network.credential)
        if let client = model.client {
            let credential = self.credential
            let fresh = await LoadState.from { try await client.awsOptions(credential: credential) }
            if case .loaded = fresh { options = fresh }
        }
        if subnetId == network.subnetId {
            groupIds = [network.securityGroupId]
            publicIp = true
        } else {
            pendingGroup = network.securityGroupId
            subnetId = network.subnetId
        }
    }

    /// The Price List's answer for the picked type; a failure (the demo has no keys) is "price unavailable".
    private func loadPrice() async {
        price = nil
        guard !typeName.isEmpty else { return }
        let credential = self.credential
        let type = typeName
        let answer = (try? await TalosClient.awsPrice(credential: credential, instanceType: type)) ?? AWSPrice()
        reseal(answer.credential)
        if !Task.isCancelled { price = answer }
    }

    /// The throwaway instance's run: followed like a join, with no node to add at its end.
    private func startAMI(_ spec: AWSImageSpec) {
        guard let client = model.client else { return }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: "", hostname: "Talos \(spec.arch)", disk: "",
            staticAddress: "", awsImage: spec)
        Task {
            let token = await runCredential()
            job.start(client: client, request: request, providerToken: token) { _ in }
        }
    }

    /// The region's AMIs again, with the new one picked.
    private func amiClosed(_ made: MadeImage) async {
        await loadOptions()
        guard case .loaded(let value, _, _) = options, value.images.contains(where: { $0.id == made.id }) else {
            return
        }
        imageId = made.id
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
        let token = await runCredential()
        job.start(client: client, request: request, providerToken: token) { address in
            try await model.addNodes([address], to: context)
        }
    }
}
