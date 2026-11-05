import IchorCore
import SwiftUI

/// "Make a Talos snapshot": the architecture (that of the server type asked for, editable), the
/// location and the cost line, then a plain confirmation: nothing of the cluster is touched and the
/// server is deleted afterwards.
struct HetznerSnapshotSheet: View {
    let options: HetznerOptions
    let token: String
    let onStart: (HetznerSnapshotSpec) -> Void
    let template: String

    @Environment(\.dismiss) private var dismiss
    @State private var arch: String
    @State private var location: String
    @State private var cost: LoadState<HetznerSnapshotCost>?

    private static let architectures = ["amd64", "arm64"]

    // Explicit: the private @State makes the memberwise init private.
    init(
        options: HetznerOptions, arch: String, location: String, template: String, token: String,
        onStart: @escaping (HetznerSnapshotSpec) -> Void
    ) {
        self.options = options
        self.template = template
        self.token = token
        self.onStart = onStart
        _arch = State(initialValue: arch)
        _location = State(initialValue: location)
    }

    private var chosenLocation: String {
        options.locations(forArch: arch).contains(where: { $0.name == location }) ? location : ""
    }

    private var ready: Bool {
        if case .loaded = cost { return true }
        return false
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "Ichor creates a throwaway server in Hetzner's rescue system, writes this cluster's Talos image to its disk, takes a snapshot, then deletes the server and its temporary SSH key. Nothing of the cluster is touched. It takes a few minutes."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                    Picker("Architecture", selection: $arch) {
                        ForEach(Self.architectures, id: \.self) { Text(verbatim: $0).tag($0) }
                    }
                    Picker("Location", selection: $location) {
                        Text(verbatim: "—").tag("")
                        ForEach(options.locations(forArch: arch)) { option in
                            Text(verbatim: "\(option.name) · \(option.city)").tag(option.name)
                        }
                    }
                }
                if let cost {
                    Section { costRow(cost) }
                }
            }
            .navigationTitle("Make a Talos snapshot")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Make the snapshot") {
                        onStart(HetznerSnapshotSpec(templateNode: template, arch: arch, location: chosenLocation))
                        dismiss()
                    }
                    .disabled(!ready)
                }
            }
            .task(id: "\(arch)/\(chosenLocation)") { await loadCost() }
        }
    }

    @ViewBuilder
    private func costRow(_ state: LoadState<HetznerSnapshotCost>) -> some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity)
        case .failed(let error):
            Text(verbatim: error).foregroundStyle(.statusBad)
            Button("Retry") { Task { await loadCost() } }
        case .loaded(let value, _, _):
            let hourly = "\(value.hourly) \(value.currency)"
            let storage = "\(value.monthlyPerGB) \(value.currency)"
            Text(
                "About one started hour of \(value.serverType) (\(hourly)) plus snapshot storage at \(storage) per GB per month, before tax"
            )
            .font(.footnote).foregroundStyle(.orange)
        }
    }

    private func loadCost() async {
        let (arch, location, token) = (arch, chosenLocation, token)
        guard !location.isEmpty else {
            cost = nil
            return
        }
        cost = .loading
        cost = await LoadState.from {
            try await TalosClient.hetznerSnapshotEstimate(token: token, arch: arch, location: location)
        }
    }
}

/// The followed image run (a Hetzner snapshot or an AWS AMI): its steps with the command's last
/// line, Stop, then the result.
struct ImageRunSections: View {
    /// Called when the result is closed, with how the run ended.
    let onClose: (JoinJob.Outcome?) -> Void

    @State private var confirmStop = false
    private var job: JoinJob { .shared }
    private var aws: Bool { job.request?.awsImage != nil }

    // Explicit: the private @State makes the memberwise init private.
    init(onClose: @escaping (JoinJob.Outcome?) -> Void) {
        self.onClose = onClose
    }

    var body: some View {
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(
                    label: step.phase.localizedLabel(awsImage: aws), state: step.state, at: step.at,
                    message: step.message)
            }
        } header: {
            // Literals in each branch: a ternary would make a String, shown unlocalized.
            if aws {
                Text("Make a Talos AMI")
            } else {
                Text("Make a Talos snapshot")
            }
        }
        Section {
            switch job.outcome {
            case .snapshotMade(let made)?:
                Text("Snapshot \(made.description) is ready: pick it to create the server.")
                closeButton()
            case .amiMade(let made)?:
                Text("AMI \(made.name) is ready: it is picked for the new node.")
                closeButton()
            case .failed(let error)?:
                Text(verbatim: error).foregroundStyle(.statusBad)
                closeButton()
            case .joined?, .appearedInOmni?:
                closeButton()
            case nil:
                Button(
                    job.stopping ? String(localized: "Stopping…") : String(localized: "Stop and clean up"),
                    role: .destructive
                ) {
                    confirmStop = true
                }
                .disabled(job.stopping)
            }
        } footer: {
            if job.isActive, aws {
                Text("Stopping terminates the throwaway instance.")
            } else if job.isActive {
                Text("Stopping deletes the throwaway server and its key.")
            }
        }
        .alert(Text("Stop and clean up"), isPresented: $confirmStop) {
            Button("Stop and clean up", role: .destructive) { job.requestStop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            if aws {
                Text(
                    "The run stops. The throwaway instance is terminated, and an unfinished snapshot or AMI is deleted."
                )
            } else {
                Text("The run stops. The throwaway server, its key and an unfinished snapshot are deleted.")
            }
        }
    }

    private func closeButton() -> some View {
        Button("Close") {
            let outcome = job.outcome
            job.clear()
            onClose(outcome)
        }
    }
}
