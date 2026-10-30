import SwiftUI
import IchorCore

/// Step 3 of "Add a node…": what is about to happen and the warnings, then "Join" behind the
/// typed hostname (and the app lock); the followed run's timeline replaces it once started.
/// `request` is nil until steps 1 and 2 are done. Sections of JoinNodeView's list.
struct JoinApplySection: View {
    let request: JoinRequest?
    let onOpenNode: (NodeRef) -> Void

    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @State private var warnings: [String] = []
    @State private var confirming = false
    @State private var authError: String?
    @State private var confirmStop = false
    @State private var replacing = false
    private var job: JoinJob { .shared }

    var body: some View {
        if job.request != nil {
            runSections
        } else {
            Section {
                if let request {
                    summary(request)
                } else {
                    Text("Inspect a node in maintenance mode and preview its config first.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            } header: {
                Text("3. Apply it and follow the node until it joins the cluster")
            }
            .task(id: request?.role) { await loadWarnings() }
            .sheet(isPresented: $confirming) {
                if let request {
                    HostnameConfirmationSheet(
                        title: String(localized: "Join \(request.hostname)?"),
                        message: String(
                            localized:
                                "The config goes to \(request.address), which installs Talos to \(request.disk) and erases it."
                        ),
                        hostname: request.hostname,
                        actionTitle: String(localized: "Join")
                    ) {
                        confirming = false
                        Task { await join(request.withReplacing(replacing)) }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func summary(_ request: JoinRequest) -> some View {
        LabeledContent("Address", value: request.address).monospaced()
        LabeledContent(
            "Role",
            value: request.role == "controlplane" ? String(localized: "Control plane") : String(localized: "Worker"))
        LabeledContent("Host name", value: request.hostname).monospaced()
        LabeledContent("Install disk", value: request.disk).monospaced()
        LabeledContent("Template", value: request.template).monospaced()
        if !request.staticAddress.isEmpty {
            LabeledContent("Static address (optional)", value: request.staticAddress).monospaced()
        }
        ForEach(warnings, id: \.self) { warning in
            Label {
                Text(verbatim: warning).font(.footnote)
            } icon: {
                Image(systemName: "exclamationmark.triangle").foregroundStyle(.statusWarn)
            }
        }
        VStack(alignment: .leading, spacing: 3) {
            Toggle("Replaces a node with this host name", isOn: $replacing)
            Text(
                "For a machine that takes the place of \(request.hostname) while cluster discovery still lists the old one. Its old addresses are never followed."
            )
            .font(.caption).foregroundStyle(.secondary)
        }
        if let authError {
            Text(verbatim: authError).foregroundStyle(.statusBad)
        }
        Button("Join", role: .destructive) { confirming = true }
    }

    @ViewBuilder private var runSections: some View {
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
            Text("3. Apply it and follow the node until it joins the cluster")
        }
        Section {
            switch job.outcome {
            case .joined(let address)?:
                let hostname = job.request?.hostname ?? ""
                Label {
                    Text("\(hostname) joined the cluster as \(address)")
                } icon: {
                    Image(systemName: "checkmark.seal.fill").foregroundStyle(.statusOK)
                }
                Button("Open node") {
                    let role = job.request?.role ?? "worker"
                    job.clear()
                    onOpenNode(NodeRef(address: address, hostname: hostname, role: role))
                }
            case .failed(let error)?:
                Label {
                    Text(verbatim: error)
                } icon: {
                    Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad)
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
            }
        } footer: {
            if job.isActive {
                Text("Once the config is sent, the install cannot be undone: stopping only stops following it.")
            }
        }
        .alert(Text("Stop following"), isPresented: $confirmStop) {
            Button("Stop following", role: .destructive) { job.requestStop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("\(job.request?.hostname ?? "") goes on installing and appears on the overview once it joins.")
        }
    }

    private func loadWarnings() async {
        guard let role = request?.role else { return }
        warnings = (try? await TalosClient.nodeJoinWarnings(role: role)) ?? []
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func join(_ request: JoinRequest) async {
        guard let client = model.client else { return }
        authError = nil
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Join \(request.hostname)?"))
        {
            authError = failure
            return
        }
        // The cluster the node joins, even if another one is active when it has joined.
        let context = model.activeContext
        job.start(client: client, request: request) { address in
            try await model.addNodes([address], to: context)
        }
    }
}

extension JoinPhase {
    var localizedLabel: String {
        switch self {
        case .applying: String(localized: "Send the config")
        case .installing: String(localized: "Install")
        case .rebooting: String(localized: "Reboot")
        case .booted: String(localized: "Booted")
        case .joining: String(localized: "Join the cluster")
        case .registering: String(localized: "Add to the cluster's nodes")
        case .done: String(localized: "Done")
        }
    }
}
