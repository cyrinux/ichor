import IchorCore
import SwiftUI

/// "Create the Instance at Scaleway": the run makes the user data itself. On Omni only a name is
/// asked; on plain Talos the card's new member is used. The cost, the typed name and the app lock
/// come before the billable create; the run's timeline then replaces the form. Sections of
/// BootMediaView's list.
struct ScalewayCreateSection: View {
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
    @State private var options: LoadState<ScalewayOptions>?
    /// The picked zone; "" until one is picked (the key's own zone is shown).
    @State private var zone = ""
    @State private var image: ScalewayImage?
    @State private var type: ScalewayType?
    @State private var group: ScalewaySecurityGroup?
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

    /// The stored key, in the picked zone when one is picked.
    private var credential: String {
        guard !account.isEmpty else { return "" }
        let stored = CloudProviderStore.token(provider: CloudProviderStore.scaleway, project: account)
        return ScalewayCredential.json(stored, inZone: zone)
    }

    private var demo: Bool { model.activeSummary?.demo == true }
    private var name: String { omni ? omniName.trimmingCharacters(in: .whitespaces) : hostname }

    var body: some View {
        if job.request?.scaleway != nil {
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
                Text("Create the Instance at Scaleway")
            }
            .task { await load() }
            .sheet(isPresented: $adding) {
                ScalewayKeySheet { added in
                    reloadAccounts()
                    pick(added)
                }
            }
            if job.request?.scalewayImage != nil {
                ImageRunSections { outcome in
                    guard case .imageMade(let made)? = outcome else { return }
                    Task { await imageClosed(made) }
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
        Button("Add a Scaleway API key") { adding = true }
        if !account.isEmpty {
            Button("Remove this key", role: .destructive) {
                try? CloudProviderStore.save(provider: CloudProviderStore.scaleway, project: account, token: "")
                reloadAccounts()
                pick(accounts.first ?? "")
            }
        }
    }

    @ViewBuilder
    private func optionsSection(_ state: LoadState<ScalewayOptions>) -> some View {
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
    private func instanceForm(_ value: ScalewayOptions) -> some View {
        let shownZone = zone.isEmpty ? value.zone : zone
        if omni {
            TextField("Host name", text: $omniName)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
        Picker("Zone", selection: Binding(get: { shownZone }, set: { pickZone($0) })) {
            ForEach(value.zones, id: \.self) { Text(verbatim: $0).tag($0) }
        }
        ForEach(value.warnings, id: \.self) { warning in
            Text(verbatim: warning).font(.footnote).foregroundStyle(.secondary)
        }
        // The image is made for the picked type's architecture, else the picked image's, else the zone's first.
        let makeArch = ScalewayArch.normalized(type?.arch ?? image?.arch ?? "")
        let noTalosImage = !value.hasTalosImage(arch: makeArch)
        if noTalosImage { makeImageRows(value, zone: shownZone, arch: makeArch, missing: true) }
        Picker("Talos image", selection: $image) {
            Text(verbatim: "—").tag(ScalewayImage?.none)
            ForEach(value.imagesTalosFirst) { option in
                Text(verbatim: "\(option.name) · \(option.arch)").tag(ScalewayImage?.some(option))
            }
        }
        if !noTalosImage { makeImageRows(value, zone: shownZone, arch: makeArch, missing: false) }
        let chosenImage = value.images.first { $0.id == image?.id }
        let types = value.types(for: chosenImage)
        Picker("Type", selection: $type) {
            Text(verbatim: "—").tag(ScalewayType?.none)
            ForEach(types) { option in
                typeLabel(option).tag(ScalewayType?.some(option))
            }
        }
        if let chosenImage {
            Text("Only the types that boot a \(chosenImage.arch) image are listed.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        if !demo && !account.isEmpty {
            ScalewayGuidedNetworkRows(
                zone: shownZone, omni: omni, role: role, credential: credential, disabled: job.isActive
            ) { network in
                Task { await refreshOptions(selecting: network.securityGroupId) }
            }
        }
        if !value.securityGroups.isEmpty {
            Picker("Security group", selection: $group) {
                ForEach(value.securityGroups) { Text(verbatim: $0.name).tag(ScalewaySecurityGroup?.some($0)) }
            }
            .onAppear {
                if group == nil { group = value.defaultSecurityGroup }
            }
        }
        // A type of another architecture than the image is not offered: it no longer counts.
        let chosenType = types.first { $0.name == type?.name }
        let chosenGroup = value.securityGroups.first { $0.id == group?.id } ?? value.defaultSecurityGroup
        // The managed group already lets this phone in; any other must be checked by hand.
        if !omni, chosenGroup?.managed != true, !value.groupWarning.isEmpty {
            Text(verbatim: value.groupWarning).font(.footnote).foregroundStyle(.secondary)
        }
        if let chosenType {
            Group {
                if let hourly = chosenType.hourlyPrice {
                    Text("About \(hourly) per hour, before tax")
                } else {
                    Text("Price unavailable: see Scaleway's price list.")
                }
            }
            .font(.headline).foregroundStyle(.orange)
        }
        if let authError {
            Text(verbatim: authError).foregroundStyle(.statusBad)
        }
        createButton(zone: shownZone, type: chosenType, image: chosenImage, group: chosenGroup)
    }

    /// "Make a Talos image" first when no Talos image of `arch` is listed, "Make another" after the
    /// picker otherwise. The demo opens the sheet too: the core refuses its run.
    @ViewBuilder
    private func makeImageRows(_ value: ScalewayOptions, zone: String, arch: String, missing: Bool) -> some View {
        let shownArch = arch.isEmpty ? (value.imageArches.first ?? "") : arch
        if demo || !account.isEmpty {
            // No Talos image at all: the core's warning above already says so.
            if missing, value.hasTalosImage(arch: "") {
                Text("No Talos image for \(shownArch) in this zone yet: Ichor can make one of this cluster's image.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Group {
                if missing {
                    Button("Make a Talos image") { makingImage = true }
                } else {
                    Button("Make another Talos image") { makingImage = true }
                }
            }
            .disabled(job.isActive || shownArch.isEmpty)
            .sheet(isPresented: $makingImage) {
                ScalewayImageSheet(
                    zone: zone, arches: value.imageArches, arch: shownArch, template: omni ? "" : template,
                    credential: credential, demo: demo
                ) { spec in
                    startImage(spec)
                }
            }
        }
    }

    private func typeLabel(_ option: ScalewayType) -> Text {
        let label = "\(option.name) · \(option.vcpus) CPU · \(option.ramGB) GB"
        return option.blockOnly ? Text("\(label) · Block Storage") : Text(verbatim: label)
    }

    private func createButton(
        zone: String, type: ScalewayType?, image: ScalewayImage?, group: ScalewaySecurityGroup?
    ) -> some View {
        Button("Create the server", role: .destructive) { confirming = true }
            .disabled(
                name.isEmpty || (!omni && (template.isEmpty || disk.isEmpty)) || zone.isEmpty || type == nil
                    || image == nil || job.isActive
            )
            .sheet(isPresented: $confirming) {
                if let type, let image {
                    HostnameConfirmationSheet(
                        title: String(localized: "Create \(name) at Scaleway?"),
                        message: String(
                            localized:
                                "This creates a billable Instance (\(type.name) in \(zone)) and a data volume. Its user data goes to Scaleway with the cluster's secrets or Omni's join token. A failed run deletes the Instance and its volumes."
                        ),
                        hostname: name,
                        actionTitle: String(localized: "Create the server")
                    ) {
                        confirming = false
                        Task { await create(zone: zone, type: type, image: image, group: group) }
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
            Text("Create the Instance at Scaleway")
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
                "\(job.request?.hostname ?? "") is deleted at Scaleway with its volumes. A control plane that already answered is kept: its etcd member must go first."
            )
        }
    }

    private func reloadAccounts() {
        accounts = CloudProviderStore.projects(provider: CloudProviderStore.scaleway)
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
        zone = ""
        resetPicks()
        if newAccount.isEmpty {
            options = nil
        } else {
            Task { await loadOptions() }
        }
    }

    /// Options are read per zone; the demo keeps its samples and only moves the picked zone.
    private func pickZone(_ newZone: String) {
        zone = newZone
        resetPicks()
        if !demo { Task { await loadOptions() } }
    }

    private func resetPicks() {
        image = nil
        type = nil
        group = nil
    }

    private func loadOptions() async {
        guard let client = model.client else { return }
        let credential = self.credential
        options = .loading
        options = await LoadState.from { try await client.scalewayOptions(credential: credential) }
    }

    /// The options again without the loading state, so the form keeps its picks, with the guided
    /// group `id` picked once it is listed; a failure keeps the old options.
    private func refreshOptions(selecting id: String) async {
        guard let client = model.client else { return }
        let credential = self.credential
        let result = await LoadState.from { try await client.scalewayOptions(credential: credential) }
        guard case .loaded(let value, _, _) = result else { return }
        options = result
        if let made = value.securityGroups.first(where: { $0.id == id }) { group = made }
    }

    /// The helper Instance's run: followed like a join, with no node to add at its end.
    private func startImage(_ spec: ScalewayImageSpec) {
        guard let client = model.client else { return }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: "", hostname: "Talos \(spec.arch)", disk: "",
            staticAddress: "", scalewayImage: spec)
        job.start(client: client, request: request, providerToken: credential) { _ in }
    }

    /// The zone's images again, with the new one picked (and a type of its architecture).
    private func imageClosed(_ made: MadeImage) async {
        await refreshOptions(selecting: group?.id ?? "")
        guard case .loaded(let value, _, _) = options, let picked = value.images.first(where: { $0.id == made.id })
        else { return }
        if let type, ScalewayArch.normalized(type.arch) != ScalewayArch.normalized(picked.arch) { self.type = nil }
        image = picked
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func create(zone: String, type: ScalewayType, image: ScalewayImage, group: ScalewaySecurityGroup?) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name) at Scaleway?"))
        {
            authError = failure
            return
        }
        let spec = ScalewayCreateSpec(
            zone: zone, type: type.name, imageId: image.id, securityGroupId: group?.id ?? "", hostname: name,
            role: omni ? "" : role, templateNode: omni ? "" : template, installDisk: omni ? "" : disk,
            labelsJson: omni ? labels : "")
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: spec.role, hostname: name, disk: spec.installDisk,
            staticAddress: "", scaleway: spec, omni: omni)
        // The cluster the node joins, even if another one is active when it has joined.
        let context = model.activeContext
        job.start(client: client, request: request, providerToken: credential) { address in
            try await model.addNodes([address], to: context)
        }
    }
}
