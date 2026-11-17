import SwiftUI
import IchorCore

/// The new cluster's control-plane config, its secrets hidden (NewClusterPrepare's preview).
struct NewClusterPreviewSheet: View {
    let yaml: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView([.horizontal, .vertical]) {
                Text(verbatim: yaml)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .padding()
            }
            .navigationTitle("Machine config")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

/// The app's root while a new-cluster run is held, else `content`: storing the first cluster's
/// talosconfig switches the root away from the import screen, so the run stays on screen here
/// until it is closed.
struct NewClusterRoot<Content: View>: View {
    @ViewBuilder let content: () -> Content

    var body: some View {
        if JoinJob.shared.request?.newClusterName != nil {
            NavigationStack { NewClusterView() }
        } else {
            content()
        }
    }
}

/// The followed new-cluster run: its timeline, then the cluster or why it stopped. A run that sent
/// nothing offers to remove the context it stored.
struct NewClusterRunSections: View {
    let request: JoinRequest
    /// The cluster's name, as its context is stored.
    let name: String

    @Environment(AppModel.self) private var model
    @State private var confirmStop = false
    @State private var removeError: String?
    private var job: JoinJob { .shared }

    private var atHetzner: Bool { request.hetznerCluster != nil }

    private func reached(_ phase: JoinPhase) -> Bool { job.events.contains { $0.phase == phase.rawValue } }

    /// The config reached the machine (at Hetzner: etcd is being bootstrapped): the cluster may
    /// exist, the stored entry is its access.
    private var sent: Bool { reached(atHetzner ? .bootstrapping : .installing) }

    /// A context was stored for the cluster (at Hetzner, only once the run handed the talosconfig over).
    private var stored: Bool { !atHetzner || reached(.secrets) }

    var body: some View {
        if job.isActive {
            Section {
                Label {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Keep Ichor open until the node joins").fontWeight(.semibold)
                        Text("The install goes on without Ichor, but in the background iOS pauses following it.")
                            .font(.footnote).foregroundStyle(.secondary)
                    }
                } icon: {
                    Image(systemName: "iphone").foregroundStyle(.orange)
                }
            }
        }
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(label: step.phase.localizedLabel, state: step.state, at: step.at, message: step.message)
            }
        } header: {
            Text("3. Store the access, install and bootstrap")
        }
        Section {
            outcome
        } footer: {
            if job.isActive && atHetzner {
                Text("Stopping before etcd is bootstrapped deletes the server and the Primary IP this run made.")
            } else if job.isActive {
                Text("Once the config is sent, the install cannot be undone: stopping only stops following it.")
            }
        }
        .alert(Text("Stop following"), isPresented: $confirmStop) {
            Button("Stop following", role: .destructive) { job.requestStop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            if atHetzner {
                Text("Stopping before etcd is bootstrapped deletes the server and the Primary IP this run made.")
            } else {
                Text("\(name) goes on installing and appears on the overview once it joins.")
            }
        }
    }

    @ViewBuilder private var outcome: some View {
        switch job.outcome {
        case .joined(let address)?:
            Label {
                Text("\(name) is up: its control plane answers at \(address)")
            } icon: {
                Image(systemName: "checkmark.seal.fill").foregroundStyle(.statusOK)
            }
            Button("Open the cluster") { job.clear() }
        case .failed(let error)?:
            Label {
                Text(verbatim: error)
            } icon: {
                Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad)
            }
            if !sent && stored && request.address != demoMaintenanceAddress {
                if atHetzner {
                    Text("The cluster was not bootstrapped: the talosconfig stored for it is of no use.")
                        .font(.footnote).foregroundStyle(.secondary)
                } else {
                    Text("Nothing reached the machine: the talosconfig stored for this cluster is of no use.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                Button("Remove the cluster entry", role: .destructive) { Task { await removeEntry() } }
            }
            if let removeError {
                Text(verbatim: removeError).foregroundStyle(.statusBad)
            }
            Button("Close") { job.clear() }
        case nil:
            Button(
                job.stopping ? String(localized: "Stopping…") : String(localized: "Stop following"),
                role: .destructive
            ) {
                confirmStop = true
            }
            .disabled(job.stopping)
        default:
            Button("Close") { job.clear() }
        }
    }

    private func removeEntry() async {
        do {
            try await model.removeCluster(name)
            job.clear()
        } catch {
            removeError = error.localizedDescription
        }
    }
}
