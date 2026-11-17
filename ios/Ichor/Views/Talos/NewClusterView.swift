import SwiftUI
import IchorCore

/// "New Talos cluster": a machine booted from the Talos ISO on the local network becomes the first
/// control plane of a new cluster. Inspect it, prepare (the secrets stay in the Go core), confirm
/// by typing the cluster's name; the talosconfig is stored, then JoinJob follows the run. While
/// that run is held, IchorApp shows this view at the root: storing the first cluster switches the
/// root away from the import screen.
struct NewClusterView: View {
    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @State private var address = ""
    @State private var inspection: LoadState<MaintenanceInspection>?
    @State private var inspecting = 0
    @State private var name = ""
    @State private var disk = ""
    @State private var kubernetesVersion = ""
    @State private var schedule = true
    @State private var draft: LoadState<NewClusterDraft>?
    @State private var startError: String?
    @State private var confirming = false
    @State private var showingConfig = false
    @State private var atHetzner = false
    private var job: JoinJob { .shared }

    var body: some View {
        List {
            if let request = job.request, let name = request.newClusterName {
                NewClusterRunSections(request: request, name: name)
            } else {
                Section {
                    Picker("Where is the machine", selection: $atHetzner) {
                        Text("On my network").tag(false)
                        Text("A server at Hetzner").tag(true)
                    }
                    .pickerStyle(.segmented)
                } header: {
                    Text("Where is the machine")
                }
                if atHetzner {
                    HetznerNewClusterSection()
                } else {
                    machineSections
                    if let machine { clusterSection(machine) }
                    draftSections
                }
            }
        }
        .navigationTitle("New Talos cluster")
        .sheet(isPresented: $confirming) {
            HostnameConfirmationSheet(
                title: String(localized: "Create \(name)?"),
                message: String(
                    localized:
                        "This erases \(disk) on \(joinAddress(address)) and makes it the only control plane of \(name). Its talosconfig is stored on this phone first."
                ),
                hostname: name,
                actionTitle: String(localized: "Create the cluster")
            ) {
                confirming = false
                Task { await create() }
            }
        }
        .sheet(isPresented: $showingConfig) {
            if case .loaded(let prepared, _, _) = draft {
                NewClusterPreviewSheet(yaml: prepared.redactedControlplane)
            }
        }
    }

    private static func isLoading<T>(_ state: LoadState<T>?) -> Bool {
        if case .loading? = state { return true }
        return false
    }

    /// The inspected machine, when it is in maintenance mode.
    private var machine: MaintenanceInspection? {
        if case .loaded(let node, _, _) = inspection, node.maintenance { return node }
        return nil
    }

    @ViewBuilder private var machineSections: some View {
        Section {
            Text(
                "Boot a machine on your local network from the Talos ISO and type the address its console shows. This phone makes the cluster's secrets and configs, sends the config to the machine and bootstraps it: no laptop needed."
            )
            .font(.footnote).foregroundStyle(.secondary)
            Text(
                "In the demo, type demo as the address: the preview works, creating the cluster is refused."
            )
            .note()
            HStack {
                TextField("Node address", text: $address, prompt: Text(verbatim: "192.168.1.42"))
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.go)
                    .onSubmit { Task { await inspect() } }
                Button("Inspect") { Task { await inspect() } }
                    .disabled(joinAddress(address).isEmpty || Self.isLoading(inspection))
            }
        } header: {
            Text("1. Inspect the new node")
        }
        .onChange(of: address) {
            inspection = nil
            draft = nil
        }
        if let inspection {
            Section {
                switch inspection {
                case .loading:
                    ProgressView().frame(maxWidth: .infinity)
                case .failed(let error):
                    Text(verbatim: error).foregroundStyle(.statusBad)
                    Button("Retry") { Task { await inspect() } }
                case .loaded(let node, _, _):
                    LabeledContent(node.address, value: node.maintenance ? "Talos \(node.version)" : "")
                        .monospaced()
                    if !node.maintenance {
                        StatusPill(label: String(localized: "Already installed"), color: .orange)
                    }
                }
            }
        }
    }

    private func clusterSection(_ machine: MaintenanceInspection) -> some View {
        Section {
            TextField("Cluster name", text: $name)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            TextField("Install disk", text: $disk, prompt: Text(verbatim: "/dev/sda"))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .monospaced()
            let disks = installDisks(machine.disks)
            if !disks.isEmpty {
                Picker("Install disk", selection: $disk) {
                    ForEach(disks, id: \.self) { Text(verbatim: $0).monospaced().tag($0) }
                }
            }
            TextField(
                "Kubernetes version", text: $kubernetesVersion, prompt: Text("Default for this Talos")
            )
            .keyboardType(.numbersAndPunctuation)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            VStack(alignment: .leading, spacing: 3) {
                Toggle("Run workloads on the control plane", isOn: $schedule)
                Text("Needed while the cluster has a single node.").font(.caption).foregroundStyle(.secondary)
            }
            Button("Prepare the cluster") { Task { await prepare(machine) } }
                .disabled(name.isEmpty || disk.isEmpty || Self.isLoading(draft))
        } header: {
            Text("2. The cluster: its name, the install disk and Kubernetes")
        }
        .onChange(of: name) { draft = nil }
        .onChange(of: disk) { draft = nil }
        .onChange(of: kubernetesVersion) { draft = nil }
        .onChange(of: schedule) { draft = nil }
    }

    @ViewBuilder private var draftSections: some View {
        switch draft {
        case nil:
            EmptyView()
        case .loading?:
            Section { ProgressView().frame(maxWidth: .infinity) }
        case .failed(let error)?:
            Section { Text(verbatim: error).foregroundStyle(.statusBad) }
        case .loaded(let prepared, _, _)?:
            Section {
                Text(verbatim: versions(prepared)).font(.headline)
                ForEach(prepared.warnings, id: \.self) { warning in
                    Label {
                        Text(verbatim: warning).font(.footnote)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle").foregroundStyle(.statusWarn)
                    }
                }
                Text(
                    "The secrets are hidden here. They are made on this phone and leave it only inside the config sent to the machine."
                )
                .font(.footnote).foregroundStyle(.secondary)
                Button("Machine config") { showingConfig = true }
                if let startError {
                    Text(verbatim: startError).foregroundStyle(.statusBad)
                }
                Button("Create the cluster", role: .destructive) { confirming = true }
            } header: {
                Text("3. Store the access, install and bootstrap")
            }
        }
    }

    private func versions(_ prepared: NewClusterDraft) -> String {
        String(
            format: String(localized: "Talos %@ · Kubernetes %@"), prepared.talosVersion, prepared.kubernetesVersion)
    }

    /// Only the latest inspection's answer is shown: an older, slower one is dropped.
    private func inspect() async {
        let typed = joinAddress(address)
        guard !typed.isEmpty else { return }
        inspecting += 1
        let current = inspecting
        inspection = .loading
        draft = nil
        let loaded = await LoadState.from { try await TalosClient.maintenanceNodeInspect(address: typed) }
        if current == inspecting { inspection = loaded }
    }

    private func prepare(_ machine: MaintenanceInspection) async {
        startError = nil
        draft = .loading
        let spec = NewClusterSpec(
            name: name.trimmingCharacters(in: .whitespaces), endpoint: machine.address,
            kubernetesVersion: kubernetesVersion.trimmingCharacters(in: .whitespaces),
            talosVersion: machine.version, installDisk: disk.trimmingCharacters(in: .whitespaces),
            allowSchedulingOnControlPlanes: schedule)
        draft = await LoadState.from { try await TalosClient.newClusterPrepare(spec) }
    }

    /// Face ID / passcode first when the app lock is on; then the talosconfig is stored (not for
    /// the demo's machine, whose run the Go core refuses) and the run starts.
    private func create() async {
        guard let machine, case .loaded(let prepared, _, _) = draft else { return }
        guard !job.isActive else {
            startError = String(localized: "Another run is being followed: wait for it to end.")
            return
        }
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Create \(name)?"))
        {
            startError = failure
            return
        }
        let clusterName = name.trimmingCharacters(in: .whitespaces)
        if machine.address != demoMaintenanceAddress {
            do {
                let conflicts = try await TalosClient.talosImportConflicts(
                    stored: model.yaml ?? "", kube: model.kubeYAML ?? "", added: prepared.talosconfig)
                guard conflicts.isEmpty else {
                    startError = String(
                        localized: "A cluster named \(clusterName) is already on this phone: pick another name.")
                    return
                }
                try await model.save(yaml: prepared.talosconfig)
            } catch {
                startError = String(localized: "The talosconfig could not be stored: \(error.localizedDescription)")
                return
            }
        }
        let request = JoinRequest(
            address: machine.address, template: "", role: "controlplane", hostname: clusterName, disk: disk,
            staticAddress: "",
            newCluster: NewClusterStart(
                draftId: prepared.draftId, name: clusterName, talosconfig: prepared.talosconfig))
        let client = TalosClient(config: prepared.talosconfig, context: clusterName)
        job.start(client: client, request: request) { address in
            try await model.addNodes([address], to: clusterName)
        }
    }
}
