import SwiftUI
import IchorCore

/// "Create the server at Hetzner": under the user data, once it is made. On Omni only a server name
/// is asked; on plain Talos the card's new member is used. The cost, the typed name and the app lock
/// come before the billable create; the run's timeline then replaces the form. Sections of
/// BootMediaView's list.
struct HetznerCreateSection: View {
    let omni: Bool
    let template: String
    let role: String
    let hostname: String
    let disk: String

    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @State private var projects: [String] = []
    @State private var project = ""
    @State private var options: LoadState<HetznerOptions>?
    @State private var type: HetznerServerType?
    @State private var location = ""
    @State private var snapshot: HetznerSnapshot?
    @State private var omniName = ""
    @State private var confirming = false
    @State private var addingToken = false
    @State private var makingSnapshot = false
    @State private var confirmStop = false
    @State private var authError: String?
    private var job: JoinJob { .shared }

    // Explicit: the private @State makes the memberwise init private.
    init(omni: Bool, template: String, role: String, hostname: String, disk: String) {
        self.omni = omni
        self.template = template
        self.role = role
        self.hostname = hostname
        self.disk = disk
    }

    private var token: String {
        project.isEmpty ? "" : CloudProviderStore.token(provider: CloudProviderStore.hetzner, project: project)
    }

    private var demo: Bool { model.activeSummary?.demo == true }
    private var name: String { omni ? omniName.trimmingCharacters(in: .whitespaces) : hostname }

    var body: some View {
        if job.request?.hetzner != nil {
            runSections
        } else {
            Section {
                Group {
                    if omni {
                        Text(
                            "Ichor creates the server from a Talos snapshot of your project, with this join config, and follows it until the machine appears in Omni."
                        )
                    } else {
                        Text(
                            "Ichor creates the server from a Talos snapshot of your project, with this user data, and follows it until the node joins."
                        )
                    }
                }
                .font(.footnote).foregroundStyle(.secondary)
                if demo {
                    Text("Demo cluster: these are samples, and nothing is created.")
                        .font(.footnote).foregroundStyle(.secondary)
                } else {
                    tokenRows
                }
            } header: {
                Text("Create the server at Hetzner")
            }
            .task { await load() }
            .sheet(isPresented: $addingToken) {
                HetznerTokenSheet { added in
                    reloadProjects()
                    pick(added)
                }
            }
            if job.request?.snapshot != nil {
                HetznerSnapshotRunSections { made in
                    Task { await snapshotClosed(made) }
                }
            }
            if let options { optionsSection(options) }
        }
    }

    @ViewBuilder private var tokenRows: some View {
        if !projects.isEmpty {
            Picker("Project", selection: Binding(get: { project }, set: { pick($0) })) {
                ForEach(projects, id: \.self) { Text(verbatim: $0).tag($0) }
            }
        }
        Button("Add a Hetzner token") { addingToken = true }
        if !project.isEmpty {
            Button("Remove this token", role: .destructive) {
                try? CloudProviderStore.save(provider: CloudProviderStore.hetzner, project: project, token: "")
                reloadProjects()
                pick(projects.first ?? "")
            }
        }
    }

    @ViewBuilder
    private func optionsSection(_ state: LoadState<HetznerOptions>) -> some View {
        Section {
            switch state {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let error):
                Text(verbatim: error).foregroundStyle(.statusBad)
                Button("Retry") { Task { await loadOptions() } }
            case .loaded(let value, _, _):
                serverForm(value)
            }
        }
    }

    @ViewBuilder
    private func serverForm(_ value: HetznerOptions) -> some View {
        if omni {
            TextField("Host name", text: $omniName)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
        Picker("Server type", selection: $type) {
            Text(verbatim: "—").tag(HetznerServerType?.none)
            ForEach(value.serverTypes) { option in
                Text(verbatim: "\(option.name) · \(option.cores) CPU · \(Int(option.memoryGB)) GB · \(option.arch)")
                    .tag(HetznerServerType?.some(option))
            }
        }
        Picker("Location", selection: $location) {
            Text(verbatim: "—").tag("")
            ForEach(value.locations.filter { type == nil || type?.price(in: $0.name) != nil }) { option in
                Text(verbatim: "\(option.name) · \(option.city)").tag(option.name)
            }
        }
        let snapshots = value.snapshots(for: type)
        if let type, !demo, !snapshots.contains(where: \.talos) {
            Text("No Talos snapshot for \(type.arch) yet: Ichor can make one.")
                .font(.footnote).foregroundStyle(.secondary)
            Button("Make a Talos snapshot") { makingSnapshot = true }
                .disabled(job.isActive)
                .sheet(isPresented: $makingSnapshot) {
                    HetznerSnapshotSheet(
                        options: value, arch: type.arch, location: location, template: omni ? "" : template,
                        token: token
                    ) { spec in
                        startSnapshot(spec)
                    }
                }
        }
        Picker("Talos snapshot", selection: $snapshot) {
            Text(verbatim: "—").tag(HetznerSnapshot?.none)
            ForEach(snapshots) { option in
                Text(verbatim: option.description.isEmpty ? String(option.id) : option.description)
                    .tag(HetznerSnapshot?.some(option))
            }
        }
        let price = type?.price(in: location)
        if let price {
            let hourly = "\(price.hourly) \(value.currency)"
            let monthly = "\(price.monthly) \(value.currency)"
            Text("About \(hourly) per hour, \(monthly) per month, before tax")
                .font(.headline).foregroundStyle(.orange)
        }
        if let authError {
            Text(verbatim: authError).foregroundStyle(.statusBad)
        }
        let chosen = snapshots.contains(where: { $0.id == snapshot?.id }) ? snapshot : nil
        Button("Create the server", role: .destructive) { confirming = true }
            .disabled(name.isEmpty || chosen == nil || price == nil || job.isActive)
            .sheet(isPresented: $confirming) {
                if let type, let chosen, let price {
                    let cost = "\(price.monthly) \(value.currency)"
                    HostnameConfirmationSheet(
                        title: String(localized: "Create \(name) at Hetzner?"),
                        message: String(
                            localized:
                                "This creates a billable server (\(type.name) in \(location), \(cost) per month). Its user data goes to Hetzner with the cluster's secrets or Omni's join token. A failed run deletes the server."
                        ),
                        hostname: name,
                        actionTitle: String(localized: "Create the server")
                    ) {
                        confirming = false
                        Task { await create(type: type, snapshot: chosen) }
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
            Text("Create the server at Hetzner")
        }
        Section {
            switch job.outcome {
            case .joined(let address)?:
                Text("\(job.request?.hostname ?? "") joined the cluster as \(address)")
                Button("Close") { job.clear() }
            case .appearedInOmni?:
                Text("\(job.request?.hostname ?? "") appeared in Omni: allocate it there")
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
                "\(job.request?.hostname ?? "") is deleted at Hetzner. A control plane that already answered is kept: its etcd member must go first."
            )
        }
    }

    private func reloadProjects() {
        projects = CloudProviderStore.projects(provider: CloudProviderStore.hetzner)
    }

    private func load() async {
        reloadProjects()
        if demo {
            await loadOptions()
        } else if !projects.contains(project) {
            pick(projects.first ?? "")
        }
    }

    private func pick(_ newProject: String) {
        project = newProject
        type = nil
        snapshot = nil
        if newProject.isEmpty {
            options = nil
        } else {
            Task { await loadOptions() }
        }
    }

    private func loadOptions() async {
        guard let client = model.client else { return }
        let token = self.token
        options = .loading
        options = await LoadState.from { try await client.hetznerOptions(token: token) }
    }

    /// The throwaway server's run: followed like a join, with no node to add at its end.
    private func startSnapshot(_ spec: HetznerSnapshotSpec) {
        guard let client = model.client else { return }
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: "", hostname: "Talos \(spec.arch)", disk: "",
            staticAddress: "", snapshot: spec)
        job.start(client: client, request: request, hetznerToken: token) { _ in }
    }

    /// The project's snapshots again, with the new one selected.
    private func snapshotClosed(_ made: HetznerSnapshotResult?) async {
        await loadOptions()
        guard let made, case .loaded(let value, _, _) = options else { return }
        snapshot = value.snapshots.first { $0.id == made.imageId } ?? snapshot
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func create(type: HetznerServerType, snapshot: HetznerSnapshot) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name) at Hetzner?"))
        {
            authError = failure
            return
        }
        let spec = HetznerCreateSpec(
            serverType: type.name, location: location, snapshotId: snapshot.id, hostname: name,
            role: omni ? "" : role, templateNode: omni ? "" : template, installDisk: omni ? "" : disk)
        let request = JoinRequest(
            address: "", template: spec.templateNode, role: spec.role, hostname: name, disk: spec.installDisk,
            staticAddress: "", hetzner: spec, omni: omni)
        // The cluster the node joins, even if another one is active when it has joined.
        let context = model.activeContext
        job.start(client: client, request: request, hetznerToken: token) { address in
            try await model.addNodes([address], to: context)
        }
    }
}

/// A project label and its token: masked, tested, then sealed. A read-only token is not saved.
private struct HetznerTokenSheet: View {
    let onSaved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var project = ""
    @State private var token = ""
    @State private var check: LoadState<HetznerTokenCheck>?
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
                        "A Read & Write token never expires and can delete everything in its project, including its servers. It stays sealed on this device and is not in backups."
                    )
                    .font(.footnote).foregroundStyle(.orange)
                    TextField("Project name (yours, for this list)", text: $project)
                    SecureField("API token", text: $token)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: token) { check = nil }
                    Button("Test") { Task { await test() } }
                        .disabled(token.isEmpty)
                }
                if let check {
                    Section { result(check) }
                }
                if let saveError {
                    Section { Text(verbatim: saveError).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add a Hetzner token")
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
    private func result(_ state: LoadState<HetznerTokenCheck>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
        case .loaded(let value, _, _):
            if value.readOnly {
                Text("Read-only token: it can list, not create. Make a Read & Write one to create a server.")
                    .foregroundStyle(.orange)
            } else {
                let servers = String(value.servers)
                Text("Read & Write token: \(servers) servers in this project.").foregroundStyle(.statusOK)
            }
        }
    }

    private var canSave: Bool {
        guard case .loaded(let value, _, _) = check, !value.readOnly else { return false }
        return !project.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private func test() async {
        let typed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        check = .loading
        check = await LoadState.from { try await TalosClient.hetznerTest(token: typed) }
    }

    private func save() {
        let label = project.trimmingCharacters(in: .whitespaces)
        do {
            try CloudProviderStore.save(
                provider: CloudProviderStore.hetzner, project: label,
                token: token.trimmingCharacters(in: .whitespacesAndNewlines))
            onSaved(label)
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}
