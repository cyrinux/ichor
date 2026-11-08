import IchorCore
import SwiftUI
import UIKit

/// "Create the VM at Azure": the run makes the custom data itself. On Omni only a name is asked;
/// on plain Talos the card's new member is used. The price, the typed name and the app lock come
/// before the billable create; the run's timeline then replaces the form. Sections of
/// BootMediaView's list.
struct AzureCreateSection: View {
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
    @State private var subscriptions: [String] = []
    @State private var subscription = ""
    @State private var group = ""
    @State private var options: LoadState<AzureOptions>?
    @State private var imageId = ""
    @State private var sizeName = ""
    @State private var networkId = ""
    @State private var subnetId = ""
    @State private var nsgId = ""
    @State private var publicIp = true
    @State private var diskGB = String(AzureDisk.defaultGB)
    @State private var price: AzurePrice?
    @State private var omniName = ""
    @State private var confirming = false
    @State private var adding = false
    @State private var makingImage = false
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
        subscription.isEmpty ? "" : CloudProviderStore.token(provider: CloudProviderStore.azure, project: subscription)
    }

    private var stored: AzureCredential? {
        try? JSONDecoder().decode(AzureCredential.self, from: Data(credential.utf8))
    }

    /// The picked subscription's Microsoft-account sign-in, nil for a principal.
    private var signedIn: AzureCredential? {
        guard let stored, stored.microsoftAccount else { return nil }
        return stored
    }

    private var demo: Bool { model.activeSummary?.demo == true }
    private var name: String { omni ? omniName.trimmingCharacters(in: .whitespaces) : hostname }

    var body: some View {
        if job.request?.azure != nil {
            runSections
        } else {
            Section {
                if demo {
                    Text("Demo cluster: these are samples, and nothing is created.")
                        .font(.footnote).foregroundStyle(.secondary)
                } else {
                    principalRows
                }
            } header: {
                Text("Create the VM at Azure")
            }
            .task { await load() }
            .sheet(isPresented: $adding) {
                AzureAccountSheet { added in
                    reloadSubscriptions()
                    pick(added)
                }
            }
            if job.request?.azureImage != nil {
                ImageRunSections { outcome in
                    guard case .imageMade(let made)? = outcome else { return }
                    Task { await imageClosed(made) }
                }
            }
            if let options { optionsSections(options) }
        }
    }

    @ViewBuilder private var principalRows: some View {
        if !subscriptions.isEmpty {
            Picker("Subscription", selection: Binding(get: { subscription }, set: { pick($0) })) {
                ForEach(subscriptions, id: \.self) { Text(verbatim: $0).tag($0) }
            }
        }
        if let signedIn {
            Text("Signed in with a Microsoft account as \(signedIn.account)")
                .font(.footnote).foregroundStyle(.secondary)
        }
        Button("Add an Azure account") { adding = true }
        if !subscription.isEmpty {
            Button(signedIn == nil ? "Remove this principal" : "Sign out", role: .destructive) {
                try? CloudProviderStore.save(provider: CloudProviderStore.azure, project: subscription, token: "")
                reloadSubscriptions()
                pick(subscriptions.first ?? "")
            }
        }
    }

    @ViewBuilder
    private func optionsSections(_ state: LoadState<AzureOptions>) -> some View {
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

    /// The host name on Omni, the resource group, the location and the core's warnings.
    @ViewBuilder
    private func placeSection(_ value: AzureOptions) -> some View {
        Section {
            if omni {
                TextField("Host name", text: $omniName)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            Picker("Resource group", selection: Binding(get: { group }, set: { chooseGroup($0, value) })) {
                Text(verbatim: "—").tag("")
                ForEach(value.resourceGroups) { Text(verbatim: "\($0.name) · \($0.location)").tag($0.name) }
            }
            if !demo, value.locations.count > 1 {
                Picker("Location", selection: Binding(get: { value.location }, set: { switchLocation($0) })) {
                    ForEach(value.locations) { Text(verbatim: value.locationLabel($0.name)).tag($0.name) }
                }
            } else {
                LabeledContent("Location") { Text(verbatim: value.locationLabel(value.location)) }
            }
            ForEach(value.warnings, id: \.self) { warning in
                Text(verbatim: warning).font(.footnote).foregroundStyle(.orange)
            }
        }
    }

    /// The image, then the sizes that boot it (its architecture and generation).
    @ViewBuilder
    private func machineSection(_ value: AzureOptions) -> some View {
        let image = value.images.first { $0.id == imageId }
        let size = image.flatMap { picked in value.sizes(for: picked).first { $0.name == sizeName } }
        let canMake = !demo && !subscription.isEmpty
        Section {
            if value.images.isEmpty, !canMake {
                Text(
                    "No Talos image in this location: upload the Image Factory's VHD to the subscription once, or pick another location."
                )
                .font(.footnote).foregroundStyle(.orange)
            } else if value.images.isEmpty {
                let location = value.locationLabel(value.location)
                Text("No Talos image in \(location) yet: Ichor can make one of this cluster's image.")
                    .font(.footnote).foregroundStyle(.secondary)
            } else {
                Picker("Talos image", selection: $imageId) {
                    Text(verbatim: "—").tag("")
                    ForEach(value.images) { image in
                        let title = image.name.isEmpty ? image.id : image.name
                        Text(verbatim: "\(title) · \(image.arch) · \(image.generation)").tag(image.id)
                    }
                }
                .onChange(of: imageId) { _, new in
                    // Another architecture or generation: the picked size may not boot it.
                    guard let picked = value.images.first(where: { $0.id == new }) else { return }
                    if !value.sizes(for: picked).contains(where: { $0.name == sizeName }) { sizeName = "" }
                }
            }
            if let image {
                let sizes = value.sizes(for: image)
                if sizes.isEmpty {
                    Text("No VM size here boots this image (\(image.arch), generation \(image.generation)).")
                        .font(.footnote).foregroundStyle(.orange)
                } else {
                    Picker("VM size", selection: $sizeName) {
                        Text(verbatim: "—").tag("")
                        ForEach(sizes) { Text(verbatim: $0.label).tag($0.name) }
                    }
                }
            }
            if canMake { makeImageRows(value) }
        }
        .task(id: sizeName) { await loadPrice(location: value.location) }
        .sheet(isPresented: $makingImage) {
            AzureImageSheet(
                options: value, resourceGroup: group, arch: size?.arch ?? image?.arch ?? "", networkId: networkId,
                subnetId: subnetId, nsgId: nsgId, template: omni ? "" : template, credential: credential
            ) { spec in
                startImage(spec)
            }
        }
    }

    /// Azure has no official Talos image: offer one of the cluster's own, in the picked resource group.
    @ViewBuilder
    private func makeImageRows(_ value: AzureOptions) -> some View {
        let grouped = value.resourceGroups.contains { $0.name == group }
        if value.images.isEmpty {
            Button("Make a Talos image") { makingImage = true }
                .disabled(job.isActive || !grouped)
        } else {
            Button("Make another Talos image") { makingImage = true }
                .disabled(job.isActive || !grouped)
        }
        if !grouped {
            Text("Pick a resource group first: the image goes in it.")
                .font(.footnote).foregroundStyle(.secondary)
        }
    }

    /// The virtual network, its subnet, the security group, the public address and the disk.
    @ViewBuilder
    private func networkSection(_ value: AzureOptions) -> some View {
        let subnet = value.subnets(in: networkId).first { $0.id == subnetId }
        let nsg = value.securityGroups.first { $0.id == nsgId }
        let subnetNsg = value.securityGroups.first { $0.id == subnet?.nsgId }
        Section {
            Picker("Virtual network", selection: $networkId) {
                Text(verbatim: "—").tag("")
                ForEach(value.networks) { Text(verbatim: "\($0.name) · \($0.resourceGroup)").tag($0.id) }
            }
            .onChange(of: networkId) { subnetId = "" }
            if !networkId.isEmpty {
                Picker("Subnet", selection: $subnetId) {
                    Text(verbatim: "—").tag("")
                    ForEach(value.subnets(in: networkId)) { Text(verbatim: $0.pickerLabel).tag($0.id) }
                }
                .onChange(of: subnetId) { _, new in
                    // A NAT gateway already reaches the Internet: no address needed.
                    publicIp = !(value.subnets(in: networkId).first { $0.id == new }?.natGateway ?? false)
                }
            }
            if subnet != nil {
                Picker("Network security group", selection: $nsgId) {
                    Text(verbatim: "—").tag("")
                    ForEach(value.securityGroups) { Text(verbatim: "\($0.name) · \($0.resourceGroup)").tag($0.id) }
                }
                if (nsg ?? subnetNsg)?.wideOpen == true {
                    Text("This group lets the Internet reach port 50000, the Talos API.")
                        .font(.footnote).foregroundStyle(.orange)
                }
                Toggle("Give it a public IP address", isOn: $publicIp)
                TextField("OS disk (GB)", text: $diskGB)
                    .keyboardType(.numberPad)
                    .foregroundStyle(AzureDisk.gigabytes(diskGB) == nil ? Color.statusBad : Color.primary)
            }
        } header: {
            Text("Subnet")
        } footer: {
            if subnet != nil, nsg == nil {
                Text("None picked: the subnet's group applies, if it has one.")
            }
        }
    }

    /// The price line, then the confirmed Create.
    @ViewBuilder
    private func createSection(_ value: AzureOptions) -> some View {
        let image = value.images.first { $0.id == imageId }
        let size = image.flatMap { picked in value.sizes(for: picked).first { $0.name == sizeName } }
        let subnet = value.subnets(in: networkId).first { $0.id == subnetId }
        let osDisk = AzureDisk.gigabytes(diskGB)
        let resourceGroup = value.resourceGroups.first { $0.name == group }
        Section {
            if size != nil {
                if let price {
                    Group {
                        if price.hourly.isEmpty {
                            Text("Price unavailable: see Azure's VM pricing.")
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
                    name.isEmpty || (!omni && (template.isEmpty || disk.isEmpty)) || resourceGroup == nil
                        || size == nil || subnet == nil || osDisk == nil || job.isActive
                )
                .sheet(isPresented: $confirming) {
                    if let resourceGroup, let image, let size, let subnet, let osDisk {
                        HostnameConfirmationSheet(
                            title: String(localized: "Create \(name) at Azure?"),
                            message: String(
                                localized:
                                    "This creates a billable VM (\(size.name) in \(value.locationLabel(value.location))), billed with its disk until it is deleted. Its custom data goes to Azure with the cluster's secrets or Omni's join token. A failed run deletes the VM, its network interface and its public IP."
                            ),
                            hostname: name,
                            actionTitle: String(localized: "Create the server")
                        ) {
                            confirming = false
                            let spec = AzureCreateSpec(
                                resourceGroup: resourceGroup.name, location: value.location, size: size.name,
                                imageId: image.id, subnetId: subnet.id, securityGroupId: nsgId,
                                associatePublicIp: publicIp, osDiskGB: osDisk, hostname: name,
                                role: omni ? "" : role, templateNode: omni ? "" : template,
                                installDisk: omni ? "" : disk, labelsJson: omni ? labels : "")
                            Task { await create(spec) }
                        }
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
            Text("Create the VM at Azure")
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
                "\(job.request?.hostname ?? "") is deleted at Azure with its disk, network interface and public IP, which ends its billing. A control plane that already answered is kept: its etcd member must go first."
            )
        }
    }

    private func reloadSubscriptions() {
        subscriptions = CloudProviderStore.projects(provider: CloudProviderStore.azure)
    }

    private func load() async {
        reloadSubscriptions()
        if demo {
            await loadOptions()
        } else if !subscriptions.contains(subscription) {
            pick(subscriptions.first ?? "")
        }
    }

    private func pick(_ newSubscription: String) {
        subscription = newSubscription
        group = stored?.resourceGroup ?? ""
        resetPicks()
        if newSubscription.isEmpty {
            options = nil
        } else {
            Task { await loadOptions() }
        }
    }

    private func resetPicks() {
        imageId = ""
        sizeName = ""
        networkId = ""
        subnetId = ""
        nsgId = ""
        price = nil
    }

    /// The resource group the VM goes in: kept with the principal (Test checks it there), and the
    /// group's location listed when it is another one. The demo only remembers it.
    private func chooseGroup(_ name: String, _ value: AzureOptions) {
        group = name
        guard !subscription.isEmpty, !name.isEmpty else { return }
        let location = value.location(of: name)
        guard reseal(AzureCredential.edited(credential, resourceGroup: name, location: location)) else { return }
        if location != value.location {
            resetPicks()
            Task { await loadOptions() }
        }
    }

    /// The same principal listing another location.
    private func switchLocation(_ location: String) {
        guard !subscription.isEmpty, let principal = stored, principal.location != location,
            reseal(AzureCredential.edited(credential, location: location))
        else { return }
        resetPicks()
        Task { await loadOptions() }
    }

    /// Seals `json` (edited as JSON: a sign-in holds a token `AzureCredential` does not keep).
    private func reseal(_ json: String?) -> Bool {
        guard let json, !json.isEmpty else { return false }
        do {
            try CloudProviderStore.save(provider: CloudProviderStore.azure, project: subscription, token: json)
            return true
        } catch {
            authError = error.localizedDescription
            return false
        }
    }

    private func loadOptions() async {
        guard let client = model.client else { return }
        let credential = self.credential
        let picked = subscription
        options = .loading
        options = await LoadState.from { try await client.azureOptions(credential: credential) }
        // The core rotated a Microsoft account's refresh token: seal the new one into what is
        // stored now, under the subscription it belongs to (another may be picked meanwhile).
        guard case .loaded(let value, _, _) = options, !value.credential.isEmpty, !picked.isEmpty else { return }
        let stored = CloudProviderStore.token(provider: CloudProviderStore.azure, project: picked)
        if let json = AzureCredential.renewed(stored, from: value.credential) {
            try? CloudProviderStore.save(provider: CloudProviderStore.azure, project: picked, token: json)
        }
    }

    /// The Retail Prices API's answer for the picked size; a failure is "price unavailable".
    private func loadPrice(location: String) async {
        price = nil
        guard !sizeName.isEmpty else { return }
        let credential = self.credential
        let size = sizeName
        let answer =
            (try? await TalosClient.azurePrice(credential: credential, location: location, size: size)) ?? AzurePrice()
        if !Task.isCancelled { price = answer }
    }

    /// The helper VM's run: followed like a join, with no node to add at its end.
    private func startImage(_ spec: AzureImageSpec) {
        guard let client = model.client else { return }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: "", hostname: "Talos \(spec.arch)", disk: "",
            staticAddress: "", azureImage: spec)
        job.start(client: client, request: request, providerToken: credential) { _ in }
    }

    /// The location's images again, with the new one picked (and a size that boots it).
    private func imageClosed(_ made: MadeImage) async {
        await loadOptions()
        guard case .loaded(let value, _, _) = options, let picked = value.images.first(where: { $0.id == made.id })
        else { return }
        if !value.sizes(for: picked).contains(where: { $0.name == sizeName }) { sizeName = "" }
        imageId = made.id
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func create(_ spec: AzureCreateSpec) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name) at Azure?"))
        {
            authError = failure
            return
        }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: spec.role, hostname: name, disk: spec.installDisk,
            staticAddress: "", azure: spec, omni: omni)
        // The cluster the node joins, even if another one is active when it has joined.
        let context = model.activeContext
        job.start(client: client, request: request, providerToken: credential) { address in
            try await model.addNodes([address], to: context)
        }
    }
}
