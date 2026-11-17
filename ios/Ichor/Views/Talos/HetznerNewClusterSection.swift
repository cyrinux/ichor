import SwiftUI
import IchorCore

/// "A server at Hetzner" in NewClusterView: a project's token, a server type, a location and the
/// project's Talos snapshot become a one-node cluster on a server and a Primary IP. The cost of both,
/// the typed name and the app lock come before anything is billed; JoinJob stores the talosconfig
/// when the run hands it over, then follows the run in NewClusterRunSections.
struct HetznerNewClusterSection: View {
    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @State private var projects: [String] = []
    @State private var project = ""
    @State private var options: LoadState<HetznerOptions>?
    @State private var name = ""
    @State private var type: HetznerServerType?
    @State private var location = ""
    @State private var snapshot: HetznerSnapshot?
    @State private var cost: LoadState<HetznerNewClusterCost>?
    @State private var confirming = false
    @State private var addingToken = false
    @State private var startError: String?
    private var job: JoinJob { .shared }

    private var token: String {
        project.isEmpty ? "" : CloudProviderStore.token(provider: CloudProviderStore.hetzner, project: project)
    }

    private var demo: Bool { model.activeSummary?.demo == true }
    private var clusterName: String { name.trimmingCharacters(in: .whitespaces) }
    private var taken: Bool { model.summary?.context(named: clusterName) != nil }

    /// The config the run is started from: only the demo is told from it, and there may be none.
    private var client: TalosClient { TalosClient(config: model.yaml ?? "", context: model.activeContext) }

    var body: some View {
        Section {
            Text(
                "A project token, a server type, a location and the project's Talos snapshot: this phone reserves a Primary IP, makes the cluster's secrets for it, stores the talosconfig, then creates the server and bootstraps it. Workers come later from Add a node."
            )
            .font(.footnote).foregroundStyle(.secondary)
            .task { await load() }
            .sheet(isPresented: $addingToken) {
                HetznerTokenSheet { added in
                    reloadProjects()
                    pick(added)
                }
            }
            if demo {
                Text("Demo cluster: these are samples, and nothing is created.")
                    .font(.footnote).foregroundStyle(.secondary)
            } else {
                tokenRows
            }
        } header: {
            Text("A server at Hetzner")
        }
        if let options { optionsSection(options) }
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
                clusterForm(value)
            }
        }
    }

    @ViewBuilder
    private func clusterForm(_ value: HetznerOptions) -> some View {
        TextField("Cluster name", text: $name)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        if taken {
            Text("A cluster named \(clusterName) is already on this phone: pick another name.")
                .foregroundStyle(.statusBad)
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
        let snapshots = value.snapshots(for: type).filter(\.talos)
        if let type, snapshots.isEmpty {
            Text(
                "This project has no Talos snapshot for \(type.arch): make one from Add a node → Hetzner, then come back."
            )
            .font(.footnote).foregroundStyle(.secondary)
        }
        Picker("Talos snapshot", selection: $snapshot) {
            Text(verbatim: "—").tag(HetznerSnapshot?.none)
            ForEach(snapshots) { option in
                Text(verbatim: option.description.isEmpty ? String(option.id) : option.description)
                    .tag(HetznerSnapshot?.some(option))
            }
        }
        costRow
            .task(id: "\(type?.name ?? "")/\(location)") { await estimate() }
        if let startError {
            Text(verbatim: startError).foregroundStyle(.statusBad)
        }
        let chosen = snapshots.contains(where: { $0.id == snapshot?.id }) ? snapshot : nil
        Button("Create the cluster", role: .destructive) { confirming = true }
            .disabled(clusterName.isEmpty || taken || chosen == nil || price == nil || job.isActive)
            .sheet(isPresented: $confirming) {
                if let type, let chosen, let price {
                    HostnameConfirmationSheet(
                        title: String(localized: "Create \(clusterName)?"),
                        message: Self.confirmBody(
                            type: type.name, location: location, cost: "\(price.totalMonthly) \(price.currency)",
                            name: clusterName),
                        hostname: clusterName,
                        actionTitle: String(localized: "Create the cluster")
                    ) {
                        confirming = false
                        Task { await create(type: type, snapshot: chosen) }
                    }
                }
            }
    }

    private var price: HetznerNewClusterCost? {
        if case .loaded(let value, _, _)? = cost { return value }
        return nil
    }

    @ViewBuilder private var costRow: some View {
        switch cost {
        case nil:
            EmptyView()
        case .loading?:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error)?:
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await estimate() } }
        case .loaded(let value, _, _)?:
            Text(
                Self.costLine(
                    total: "\(value.totalMonthly) \(value.currency)",
                    server: "\(value.serverMonthly) \(value.currency)",
                    ip: "\(value.primaryIPMonthly) \(value.currency)")
            )
            .font(.headline).foregroundStyle(.orange)
        }
    }

    private static func costLine(total: String, server: String, ip: String) -> String {
        String(
            localized:
                "About \(total) a month: the server \(server), its Primary IP \(ip). Billed from creation; deleted again if the run fails before the cluster exists."
        )
    }

    private static func confirmBody(type: String, location: String, cost: String, name: String) -> String {
        String(
            localized:
                "This creates a billed \(type) server and a Primary IP in \(location) (about \(cost) a month) and makes the cluster \(name). Type the name to confirm."
        )
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
        let (config, context, token) = (model.yaml ?? "", model.activeContext, self.token)
        options = .loading
        options = await LoadState.from {
            try await TalosClient.hetznerClusterOptions(config: config, context: context, token: token)
        }
    }

    private func estimate() async {
        guard let type, type.price(in: location) != nil else {
            cost = nil
            return
        }
        let (config, context, token, serverType, place) = (
            model.yaml ?? "", model.activeContext, self.token, type.name, location
        )
        cost = .loading
        cost = await LoadState.from {
            try await TalosClient.hetznerNewClusterEstimate(
                config: config, context: context, token: token, serverType: serverType, location: place)
        }
    }

    /// Face ID / passcode first when the app lock is on, then the run. The talosconfig is stored as
    /// the run hands it over, after the same name check as a cluster made from a machine.
    private func create(type: HetznerServerType, snapshot: HetznerSnapshot) async {
        guard !job.isActive else {
            startError = String(localized: "Another run is being followed: wait for it to end.")
            return
        }
        startError = nil
        let name = clusterName
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name)?"))
        {
            startError = failure
            return
        }
        let spec = HetznerNewClusterSpec(name: name, serverType: type.name, location: location, snapshotId: snapshot.id)
        let request = JoinRequest(
            address: "", template: "", role: "controlplane", hostname: name, disk: "", staticAddress: "",
            hetznerCluster: spec)
        job.start(
            client: client, request: request, providerToken: token,
            storeTalosconfig: { talosconfig in
                let conflicts = try await TalosClient.talosImportConflicts(
                    stored: model.yaml ?? "", kube: model.kubeYAML ?? "", added: talosconfig)
                guard conflicts.isEmpty else {
                    throw NSError(
                        domain: "Ichor", code: 1,
                        userInfo: [
                            NSLocalizedDescriptionKey: String(
                                localized: "A cluster named \(name) is already on this phone: pick another name.")
                        ])
                }
                try await model.save(yaml: talosconfig)
            }
        ) { address in
            try await model.addNodes([address], to: name)
        }
    }
}
