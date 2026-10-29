import SwiftUI
import IchorCore

/// Rotates the Talos API CA of the cluster on screen (os:admin), like `talosctl rotate-ca
/// --kubernetes=false`: the plan first (the current CA, the nodes, what stands in the way), then
/// the run, which CARotationJob follows app-wide with a Live Activity. Once it is done, Issue
/// talosconfig gives the other devices a config of the new CA.
struct RotateCAView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<CARotationPlan> = .loading
    @State private var confirming = false
    @State private var stopping = false
    @State private var startError: String?

    private var job: CARotationJob { CARotationJob.shared }

    var body: some View {
        Group {
            if !model.allows(.issueConfig) {
                List { Section { RoleNotice(feature: .issueConfig, roles: model.activeSummary?.roles ?? []) } }
            } else if let run = job.state {
                runList(run)
            } else {
                LoadStateView(state: state, retry: load) { plan in planList(plan) }
            }
        }
        .themedBackground()
        .navigationTitle("Rotate the Talos CA")
        .clusterScreenTitle(Text("Rotate the Talos CA"))
        .navigationBarTitleDisplayMode(.inline)
        .task { if job.state == nil { await load() } }
        .sheet(isPresented: $confirming) {
            HostnameConfirmationSheet(
                title: String(localized: "Rotate the Talos CA of “\(model.activeContext)”?"),
                message: String(
                    localized:
                        "Each node’s machine config changes three times, without a reboot. This phone’s talosconfig is replaced on the way. Every other talosconfig for this cluster stops working at the end: issue new ones afterwards."
                ),
                hostname: model.activeContext,
                actionTitle: String(localized: "Rotate")
            ) {
                Task { await start() }
            }
        }
        .confirmationDialog("Stop the rotation?", isPresented: $stopping, titleVisibility: .visible) {
            Button("Stop", role: .destructive) { job.stop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(
                "It stops before its next step. The cluster keeps the CAs it has then, and this phone’s talosconfig keeps working. Rotate again later to finish."
            )
        }
    }

    private func planList(_ plan: CARotationPlan) -> some View {
        List {
            Section {
                Text(
                    "A new CA replaces the one that signs every Talos API certificate, like talosctl rotate-ca --kubernetes=false. The nodes trust both CAs while it goes on, and this phone stores its new talosconfig before the old CA stops issuing, so nothing is cut off on the way. At the end every certificate of the old CA stops working, other devices’ talosconfigs included. The Kubernetes CA does not change."
                )
                .font(.footnote).foregroundStyle(.secondary)
            }
            if let ca = plan.ca {
                Section("Current CA") {
                    LabeledContent("Subject") { Text(verbatim: ca.subject).font(.callout.monospaced()) }
                    LabeledContent("SHA-256") { Text(verbatim: ca.fingerprint).font(.callout.monospaced()) }
                    LabeledContent(
                        "Valid until",
                        value: Date(timeIntervalSince1970: TimeInterval(ca.notAfter)).formatted(
                            date: .abbreviated, time: .omitted))
                }
            }
            Section {
                ForEach(plan.nodes) { node in
                    nodeRow(
                        node.label,
                        detail: "\(node.node) · \(node.role) · \(node.version.isEmpty ? "?" : node.version)",
                        health: node.health, problem: node.problem)
                }
            } header: {
                HStack {
                    Text("Nodes (\(plan.nodes.count))")
                    Spacer()
                    StatusPill(
                        label: plan.ready ? String(localized: "Ready") : String(localized: "Blocked"),
                        color: plan.ready ? Color.statusOK : Color.statusBad)
                }
            } footer: {
                Text(
                    plan.discovered
                        ? LocalizedStringKey("Every member cluster discovery lists")
                        : LocalizedStringKey("The nodes this talosconfig names"))
            }
            if !plan.blockers.isEmpty {
                Section("Before it can start") {
                    ForEach(plan.blockers, id: \.self) {
                        Text(verbatim: $0).font(.footnote).foregroundStyle(.statusBad)
                    }
                }
            }
            if !plan.warnings.isEmpty {
                Section("Good to know") {
                    ForEach(plan.warnings, id: \.self) {
                        Text(verbatim: $0).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            }
            Section {
                Button("Rotate the CA…", role: .destructive) { confirming = true }
                    .disabled(!plan.ready)
                if let startError {
                    Text(verbatim: startError).font(.footnote).foregroundStyle(.statusBad)
                }
            }
        }
        .refreshable { await load() }
    }

    private func runList(_ run: CARotationJob.State) -> some View {
        List {
            if run.context != model.activeContext {
                Section { Text("This rotation is of \(run.context), not of the cluster on screen.").font(.footnote) }
            }
            Section {
                ForEach(run.timeline) { step in
                    RunStepRow(label: step.phase.localizedLabel, state: step.state, at: step.at, message: step.message)
                }
            } header: {
                Text("Rotating the CA of \(run.context)")
            }
            if let nodes = run.last?.nodes, !nodes.isEmpty {
                Section("Nodes (\(nodes.count))") {
                    ForEach(nodes) { node in
                        nodeRow(node.label, detail: "\(node.node) · \(node.role)", health: node.health, problem: "")
                    }
                }
            }
            Section { outcome(run) }
        }
    }

    @ViewBuilder
    private func outcome(_ run: CARotationJob.State) -> some View {
        if run.running {
            Text("You can leave this screen: the rotation goes on in a notification.").font(.footnote).foregroundStyle(
                .secondary)
            Button(
                run.stopping ? String(localized: "Stopping before the next step…") : String(localized: "Stop"),
                role: .destructive
            ) { stopping = true }
            .disabled(run.stopping)
        } else if let error = run.error {
            Label {
                Text(verbatim: error)
            } icon: {
                Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad)
            }
            Text(
                run.transitionStored
                    ? LocalizedStringKey(
                        "This phone already stores a talosconfig that trusts both CAs, so it keeps reaching the cluster. Some nodes may issue with the new CA already: read the plan again, then rotate again to finish."
                    )
                    : LocalizedStringKey(
                        "The issuing CA did not change and this phone’s talosconfig is the one it had. The nodes may also accept the new CA now: a new rotation starts over."
                    )
            )
            .font(.footnote).foregroundStyle(.secondary)
            Button("Done") { dismissRun() }
        } else {
            Label {
                Text("CA rotated")
            } icon: {
                Image(systemName: "checkmark.seal.fill").foregroundStyle(.statusOK)
            }
            Text(CARotationJob.doneBody).font(.footnote)
            if let failure = run.finalStoreError {
                Text(
                    "The final talosconfig could not be stored (\(failure)). The one stored during the rotation keeps working."
                )
                .font(.footnote).foregroundStyle(.secondary)
            }
            NavigationLink("Issue talosconfigs for other devices", value: Route.issueConfig(renew: false))
            Button("Done") { dismissRun() }
        }
    }

    private func nodeRow(_ name: String, detail: String, health: ServiceHealth, problem: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            HealthDot(health: health)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: name).lineLimit(1)
                Text(verbatim: detail).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                if !problem.isEmpty {
                    Text(verbatim: problem).font(.caption).foregroundStyle(.statusBad)
                }
            }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let next = await LoadState.from { try await client.caRotationPlan() }
        state = state.refreshed(with: next)
    }

    /// Face ID / passcode first when the app lock is on, then the run.
    private func start() async {
        guard let client = model.client else { return }
        startError = nil
        if model.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Rotate the cluster CA"))
        {
            startError = failure
            return
        }
        let context = model.activeContext
        job.start(client: client, context: context) { yaml in
            try await model.replaceCredentials(of: context, with: yaml)
        }
    }

    private func dismissRun() {
        job.dismiss()
        Task { await load() }
    }
}

extension CARotationPhase {
    var localizedLabel: String {
        switch self {
        case .check: String(localized: "Check every node")
        case .accept: String(localized: "Accept the new CA")
        case .handover: String(localized: "Store the new talosconfig on this phone")
        case .swap: String(localized: "Issue with the new CA")
        case .drop, .done: String(localized: "Drop the old CA")
        }
    }
}
