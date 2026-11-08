import SwiftUI
import IchorCore

/// On Omni's Boot a new machine: the role and labels signed into the join token, and what the
/// cluster's machine classes match. A section of BootMediaView's list.
struct OmniLabelsSection: View {
    @Binding var labelsJSON: String

    @Environment(AppModel.self) private var model
    @State private var role = "worker"
    @State private var extra = ""
    @State private var classLabels: [String] = []

    // Explicit: the private @State makes the memberwise init private.
    init(labelsJSON: Binding<String>) {
        _labelsJSON = labelsJSON
    }

    var body: some View {
        Section {
            Text(
                "Signed into the join token: the machine appears in Omni with them, and a machine class can pick it by them."
            )
            .font(.footnote).foregroundStyle(.secondary)
            Picker("Role", selection: $role) {
                Text("Worker").tag("worker")
                Text("Control plane").tag("controlplane")
            }
            .pickerStyle(.segmented)
            TextField("More labels (key=value, separated by commas)", text: $extra)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            if !classLabels.isEmpty {
                Text("This cluster's machine classes ask for: \(classLabels.joined(separator: "; "))")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Button("Apply the labels") { apply() }
        } header: {
            Text("The new machine's labels")
        }
        .onChange(of: role) { apply() }
        .task {
            // Only a hint next to the field: the section works without it.
            let sets = try? await model.client?.omniMachineSets()
            classLabels = Array(Set(sets?.machineSets.flatMap(\.matchLabels) ?? [])).sorted()
        }
    }

    private func apply() {
        labelsJSON = omniLabelsJSON(role: role, extra: extra)
    }
}

extension JoinJob {
    /// The screen following this run: an allocation has its own, the others are on Add a node.
    var route: Route? {
        guard let request else { return nil }
        return request.omniAllocate == nil ? .joinNode : .omniAllocate
    }
}

/// "Allocate in Omni": the machines no cluster has and the cluster's machine sets. The confirmation
/// is plain for a worker and the typed host name for a control plane, which changes etcd's quorum;
/// the followed run's timeline then replaces the form.
struct OmniAllocateView: View {
    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @State private var sets: LoadState<OmniMachineSets> = .loading
    @State private var machines: LoadState<[OmniMachine]> = .loading
    @State private var machine: OmniMachine?
    @State private var set: OmniMachineSet?
    @State private var confirmingWorker = false
    @State private var confirmingControlPlane = false
    @State private var confirmStop = false
    @State private var authError: String?
    private var job: JoinJob { .shared }

    var body: some View {
        List {
            if job.request?.omniAllocate != nil {
                runSections
            } else {
                Section {
                    Text(
                        "Machines that registered in Omni and belong to no cluster yet. Pick one, and the machine set it joins."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                }
                machinesSection
                setsSection
                actionSection
            }
        }
        .navigationTitle("Allocate in Omni")
        .clusterScreenTitle(Text("Allocate in Omni"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    @ViewBuilder private var machinesSection: some View {
        Section {
            switch machines {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let error):
                LoadFailureRow(message: error, retry: { await load() })
            case .loaded(let list, _, _):
                if list.isEmpty {
                    Text("No machine is waiting: boot one with Omni's join config and it shows up here.").note()
                }
                ForEach(list) { item in
                    machineRow(item)
                }
            }
        } header: {
            Text("Machines")
        }
    }

    private func machineRow(_ item: OmniMachine) -> some View {
        Button {
            machine = item
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: item.name).monospaced()
                    Text(verbatim: machineDetail(item)).font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                if machine?.id == item.id {
                    Image(systemName: "checkmark").accessibilityHidden(true)
                }
            }
        }
        .accessibilityAddTraits(machine?.id == item.id ? .isSelected : [])
    }

    private func machineDetail(_ item: OmniMachine) -> String {
        var parts = [item.addresses.first ?? "", item.talosVersion].filter { !$0.isEmpty }
        if item.maintenance { parts.append(String(localized: "maintenance")) }
        if !item.connected { parts.append(String(localized: "offline")) }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder private var setsSection: some View {
        Section {
            switch sets {
            case .loading:
                ProgressView().frame(maxWidth: .infinity)
            case .failed(let error):
                LoadFailureRow(message: error, retry: { await load() })
            case .loaded(let value, _, _):
                ForEach(value.machineSets) { item in
                    setRow(item)
                }
            }
        } header: {
            Text("Machine set")
        }
    }

    private func setRow(_ item: OmniMachineSet) -> some View {
        Button {
            set = item
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: item.id).monospaced()
                    if item.isControlPlane {
                        Text("Control plane").font(.caption)
                    } else {
                        Text("Worker").font(.caption)
                    }
                    allocationText(item).font(.caption).foregroundStyle(.secondary)
                    if !item.matchLabels.isEmpty {
                        Text("The class asks for: \(item.matchLabels.joined(separator: "; "))")
                            .font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if set?.id == item.id {
                    Image(systemName: "checkmark").accessibilityHidden(true)
                }
            }
        }
        .accessibilityAddTraits(set?.id == item.id ? .isSelected : [])
    }

    private func allocationText(_ item: OmniMachineSet) -> Text {
        let name = item.allocation.className
        return switch item.allocation.type {
        case "static":
            Text("Machine class \(name): its count goes up by one, and the class picks a matching machine.")
        case "unlimited":
            Text("Machine class \(name) takes every matching machine: it picks this one up with no write.")
        default:
            Text("Binds the machine to this set.")
        }
    }

    @ViewBuilder private var actionSection: some View {
        if let machine, let set {
            Section {
                if let authError {
                    Text(verbatim: authError).foregroundStyle(.statusBad)
                }
                Button("Allocate") {
                    if set.isControlPlane { confirmingControlPlane = true } else { confirmingWorker = true }
                }
                .disabled(job.isActive)
            }
            .alert(
                Text("Allocate \(machine.name)?"),
                isPresented: $confirmingWorker
            ) {
                Button("Allocate") { Task { await allocate(machine, in: set) } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("\(machine.name) joins machine set \(set.id).")
            }
            .sheet(isPresented: $confirmingControlPlane) {
                HostnameConfirmationSheet(
                    title: String(localized: "Allocate \(machine.name)?"),
                    message: String(
                        localized:
                            "\(machine.name) joins the control plane through \(set.id): the etcd quorum then counts one more member."
                    ),
                    hostname: machine.name,
                    actionTitle: String(localized: "Allocate")
                ) {
                    confirmingControlPlane = false
                    Task { await allocate(machine, in: set) }
                }
            }
        }
    }

    @ViewBuilder private var runSections: some View {
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(label: step.phase.localizedLabel, state: step.state, at: step.at, message: step.message)
            }
        }
        Section {
            switch job.outcome {
            case .failed(let error)?:
                Label {
                    Text(verbatim: error)
                } icon: {
                    Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad)
                }
                Button("Close") { close() }
            case nil:
                Button(
                    job.stopping ? String(localized: "Stopping…") : String(localized: "Stop following"),
                    role: .destructive
                ) {
                    confirmStop = true
                }
                .disabled(job.stopping)
            case .some:
                Label {
                    Text("\(job.request?.hostname ?? "") runs in the cluster")
                } icon: {
                    Image(systemName: "checkmark.seal.fill").foregroundStyle(.statusOK)
                }
                Button("Close") { close() }
            }
        } footer: {
            if job.isActive {
                Text("The allocation stays: stopping only stops following it.")
            }
        }
        .alert(Text("Stop following"), isPresented: $confirmStop) {
            Button("Stop following", role: .destructive) { job.requestStop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("\(job.request?.hostname ?? "") stays allocated, and Omni goes on with it.")
        }
    }

    private func close() {
        job.clear()
        Task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        sets = await LoadState.from { try await client.omniMachineSets() }
        machines = await LoadState.from { try await client.omniUnallocatedMachines() }
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func allocate(_ machine: OmniMachine, in set: OmniMachineSet) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Allocate \(machine.name)?"))
        {
            authError = failure
            return
        }
        let request = JoinRequest(
            address: "", template: "", role: set.role, hostname: machine.name, disk: "", staticAddress: "",
            omni: true, omniAllocate: OmniAllocateSpec(machineID: machine.id, machineSetID: set.id))
        job.start(client: client, request: request) { _ in }
    }
}
