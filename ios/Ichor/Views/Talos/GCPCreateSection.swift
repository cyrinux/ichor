import IchorCore
import SwiftUI

/// "Create the VM at Google Cloud": the run makes the user data itself. On Omni only a name is
/// asked; on plain Talos the card's new member is used. The price, the typed name and the app lock
/// come before the billable create; the run's timeline then replaces the form. Sections of
/// BootMediaView's list.
struct GCPCreateSection: View {
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
    @State private var options: LoadState<GCPOptions>?
    @State private var typeName = ""
    @State private var imageLink = ""
    @State private var networkName = ""
    @State private var subnetLink = ""
    @State private var publicIp = true
    @State private var diskGB = GCPCreateSpec.defaultDiskGB
    @State private var price: GCPPrice?
    @State private var omniName = ""
    @State private var confirming = false
    @State private var adding = false
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
        account.isEmpty ? "" : CloudProviderStore.token(provider: CloudProviderStore.gcp, project: account)
    }

    private var demo: Bool { model.activeSummary?.demo == true }
    // The VM is named after it: Compute Engine takes lowercase only.
    private var name: String { omni ? omniName.trimmingCharacters(in: .whitespaces).lowercased() : hostname }

    var body: some View {
        if job.request?.gcp != nil {
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
                Text("Create the VM at Google Cloud")
            }
            .task { await load() }
            .sheet(isPresented: $adding) {
                GCPKeySheet { added in
                    reloadAccounts()
                    pick(added)
                }
            }
            if let options { optionsSections(options) }
        }
    }

    @ViewBuilder private var accountRows: some View {
        if !accounts.isEmpty {
            Picker("Service account key (JSON)", selection: Binding(get: { account }, set: { pick($0) })) {
                ForEach(accounts, id: \.self) { Text(verbatim: $0).tag($0) }
            }
        }
        Button("Add a service account key") { adding = true }
        if !account.isEmpty {
            Button("Remove this key", role: .destructive) {
                try? CloudProviderStore.save(provider: CloudProviderStore.gcp, project: account, token: "")
                reloadAccounts()
                pick(accounts.first ?? "")
            }
        }
    }

    @ViewBuilder
    private func optionsSections(_ state: LoadState<GCPOptions>) -> some View {
        switch state {
        case .loading:
            Section { ProgressView().frame(maxWidth: .infinity) }
        case .failed(let error):
            Section {
                Text(verbatim: error).foregroundStyle(.statusBad)
                if let link = GCPNames.activationLink(in: error) {
                    Link("Open the page to enable it", destination: link)
                }
                Button("Retry") { Task { await loadOptions() } }
            }
        case .loaded(let value, _, _):
            placeSection(value)
            machineSection(value)
            networkSection(value)
            createSection(value)
        }
    }

    /// The host name on Omni, the project, the zone and the core's warnings.
    @ViewBuilder
    private func placeSection(_ value: GCPOptions) -> some View {
        Section {
            if omni {
                TextField("Host name", text: $omniName)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            if !name.isEmpty, !GCPNames.valid(name) {
                Text(
                    "The VM is named after the host name: lowercase letters, digits and hyphens, starting with a letter, at most 63."
                )
                .font(.footnote).foregroundStyle(.orange)
            }
            if !demo, value.projects.count > 1 {
                Picker("Project", selection: Binding(get: { value.project }, set: { switchPlace(project: $0) })) {
                    ForEach(value.projects) { Text(verbatim: $0.label).tag($0.id) }
                }
            } else {
                LabeledContent("Project") { Text(verbatim: value.project) }
            }
            if !demo, value.zones.count > 1 {
                Picker("Zone", selection: Binding(get: { value.zone }, set: { switchPlace(zone: $0) })) {
                    ForEach(value.zones) { Text(verbatim: $0.name).tag($0.name) }
                }
            } else {
                LabeledContent("Zone") { Text(verbatim: value.zone) }
            }
            ForEach(value.otherWarnings, id: \.self) { warning in
                Text(verbatim: warning).font(.footnote).foregroundStyle(.orange)
            }
        }
    }

    /// The machine type, grouped by arch, and the images that type boots.
    @ViewBuilder
    private func machineSection(_ value: GCPOptions) -> some View {
        let type = value.machineTypes.first { $0.name == typeName }
        Section {
            Picker("Machine type", selection: $typeName) {
                Text(verbatim: "—").tag("")
                ForEach(value.arches, id: \.self) { arch in
                    Section(arch) {
                        ForEach(value.machineTypes(for: arch)) { option in
                            Text(verbatim: "\(option.name) · \(option.vcpus) CPU · \(option.memoryLabel) GB")
                                .tag(option.name)
                        }
                    }
                }
            }
            .onChange(of: typeName) { old, new in
                // Another arch: the picked image cannot boot it.
                let arch = { (name: String) in value.machineTypes.first { $0.name == name }?.arch }
                if arch(old) != arch(new) { imageLink = "" }
            }
            if let type {
                let images = value.images(for: type)
                if images.isEmpty {
                    Text(
                        "No Talos image for \(type.arch) in this project: register one as the Talos guide says, or pick another architecture."
                    )
                    .font(.footnote).foregroundStyle(.orange)
                    Button("Make a Talos image") {}
                        .disabled(true)
                    Text("Coming next: an image of the cluster's schematic, made from the phone.")
                        .font(.footnote).foregroundStyle(.secondary)
                } else {
                    Picker("Talos image", selection: $imageLink) {
                        Text(verbatim: "—").tag("")
                        ForEach(images) { Text(verbatim: label($0)).tag($0.id) }
                    }
                }
            }
        }
        .task(id: "\(value.zone)/\(typeName)") { await loadPrice(zone: value.zone) }
    }

    /// The network, its subnet in the zone's region, the public address and the boot disk.
    @ViewBuilder
    private func networkSection(_ value: GCPOptions) -> some View {
        let subnets = value.subnets(in: networkName, region: value.region)
        Section {
            if value.usableNetworks.isEmpty {
                Text("No network has a subnet in \(value.region): pick another zone.")
                    .font(.footnote).foregroundStyle(.orange)
            } else {
                Picker("Network", selection: $networkName) {
                    Text(verbatim: "—").tag("")
                    ForEach(value.usableNetworks) { Text(verbatim: $0.name).tag($0.name) }
                }
                .onChange(of: networkName) { _, new in
                    // The only subnet of the network is the obvious pick.
                    let choices = value.subnets(in: new, region: value.region)
                    subnetLink = choices.count == 1 ? choices[0].id : ""
                }
                if !networkName.isEmpty {
                    Picker("Subnet", selection: $subnetLink) {
                        Text(verbatim: "—").tag("")
                        ForEach(subnets) { Text(verbatim: label($0)).tag($0.id) }
                    }
                }
            }
            Toggle("Give it a public address", isOn: $publicIp)
            if !publicIp {
                ForEach(value.natWarnings, id: \.self) { warning in
                    Text(verbatim: warning).font(.footnote).foregroundStyle(.orange)
                }
            }
            Stepper(value: $diskGB, in: GCPCreateSpec.diskRange, step: 10) {
                LabeledContent("Boot disk (GB)") { Text(verbatim: "\(diskGB)") }
            }
        } footer: {
            Text("From 10 to 65536 GB; 20 is enough for Talos.")
        }
    }

    /// The price line, then the confirmed Create.
    @ViewBuilder
    private func createSection(_ value: GCPOptions) -> some View {
        let type = value.machineTypes.first { $0.name == typeName }
        let image = type.flatMap { t in value.images(for: t).first { $0.id == imageLink } }
        let subnet = value.subnets(in: networkName, region: value.region).first { $0.id == subnetLink }
        Section {
            if type != nil {
                if let price {
                    Group {
                        if price.hourly.isEmpty {
                            Text("Price unavailable: see Google Cloud's Compute Engine pricing.")
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
                    !GCPNames.valid(name) || (!omni && (template.isEmpty || disk.isEmpty)) || type == nil
                        || image == nil || subnet == nil || job.isActive
                )
                .sheet(isPresented: $confirming) {
                    if let type, let image, let subnet {
                        HostnameConfirmationSheet(
                            title: String(localized: "Create \(name) at Google Cloud?"),
                            message: String(
                                localized:
                                    "This creates a billable VM (\(type.name) in \(value.zone)) with its boot disk, billed until it is deleted. Its user data goes to Google Cloud with the cluster's secrets or Omni's join token. A failed run deletes the VM."
                            ),
                            hostname: name,
                            actionTitle: String(localized: "Create the server")
                        ) {
                            confirming = false
                            let spec = GCPCreateSpec(
                                project: value.project, zone: value.zone, machineType: type.name,
                                image: image.selfLink.isEmpty ? image.name : image.selfLink,
                                subnetwork: subnet.selfLink.isEmpty ? subnet.name : subnet.selfLink,
                                associatePublicIp: publicIp, diskGB: diskGB, hostname: name,
                                role: omni ? "" : role, templateNode: omni ? "" : template,
                                installDisk: omni ? "" : disk, labelsJson: omni ? labels : "")
                            Task { await create(spec) }
                        }
                    }
                }
        }
    }

    /// "talos-v1-12-0-amd64 · v1.12.0".
    private func label(_ image: GCPImage) -> String {
        [image.name, image.talosVersion].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// "default · europe-west1 · 10.132.0.0/20".
    private func label(_ subnet: GCPSubnet) -> String {
        [subnet.name, subnet.region, subnet.cidr].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    @ViewBuilder private var runSections: some View {
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(label: step.phase.localizedLabel, state: step.state, at: step.at, message: step.message)
            }
        } header: {
            Text("Create the VM at Google Cloud")
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
                "\(job.request?.hostname ?? "") is deleted at Google Cloud with its boot disk, which ends its billing. A control plane that already answered is kept: its etcd member must go first."
            )
        }
    }

    private func reloadAccounts() {
        accounts = CloudProviderStore.projects(provider: CloudProviderStore.gcp)
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
        imageLink = ""
        networkName = ""
        subnetLink = ""
        price = nil
        if newAccount.isEmpty {
            options = nil
        } else {
            Task { await loadOptions() }
        }
    }

    /// The same key in another project or zone: re-sealed with them, then their options.
    private func switchPlace(project: String? = nil, zone: String? = nil) {
        guard !account.isEmpty,
            let current = try? JSONDecoder().decode(GCPCredential.self, from: Data(credential.utf8)),
            let json = try? JSONEncoder().encode(current.with(project: project, zone: zone))
        else { return }
        do {
            try CloudProviderStore.save(
                provider: CloudProviderStore.gcp, project: account, token: String(decoding: json, as: UTF8.self))
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
        options = await LoadState.from { try await client.gcpOptions(credential: credential) }
        // The only network is the obvious pick, and its only subnet.
        if case .loaded(let value, _, _)? = options, value.usableNetworks.count == 1 {
            networkName = value.usableNetworks[0].name
            let subnets = value.subnets(in: networkName, region: value.region)
            subnetLink = subnets.count == 1 ? subnets[0].id : ""
        }
    }

    /// The Billing Catalog's answer for the picked type; a failure (the demo has no key) is "price unavailable".
    private func loadPrice(zone: String) async {
        price = nil
        guard !typeName.isEmpty, !zone.isEmpty else { return }
        let credential = self.credential
        let type = typeName
        let answer =
            (try? await TalosClient.gcpPrice(credential: credential, zone: zone, machineType: type)) ?? GCPPrice()
        if !Task.isCancelled { price = answer }
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func create(_ spec: GCPCreateSpec) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name) at Google Cloud?"))
        {
            authError = failure
            return
        }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: spec.role, hostname: name, disk: spec.installDisk,
            staticAddress: "", gcp: spec, omni: omni)
        // The cluster the node joins, even if another one is active when it has joined.
        let context = model.activeContext
        job.start(client: client, request: request, providerToken: credential) { address in
            try await model.addNodes([address], to: context)
        }
    }
}
