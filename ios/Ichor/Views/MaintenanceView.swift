import SwiftUI
import IchorCore

/// Node maintenance (os:admin): the plan (pods the drain evicts or leaves, their budgets,
/// the reboot checks), then the run cordon → drain → reboot or shutdown → uncordon. One
/// maintenance at a time in the app. On a cluster without Talos (`node`: the Kubernetes node
/// name) it is a drain only: no action to pick, no reboot checks.
struct MaintenanceView: View {
    let node: String
    let hostname: String

    @Environment(AppModel.self) private var model
    @State private var plan: LoadState<MaintenancePlan> = .loading
    @State private var action = MaintenanceAction.reboot
    @State private var includeBare = false
    @State private var confirming = false
    @State private var message: String?

    private var job: MaintenanceJob { .shared }
    private var kube: Bool { model.activeIsKube }

    var body: some View {
        Group {
            if let target = job.target, target.node == node {
                MaintenanceRunView(job: job, hostname: hostname) { Task { await loadPlan() } }
            } else {
                LoadStateView(state: plan, retry: loadPlan) { plan in form(plan) }
            }
        }
        .navigationTitle(kube ? String(localized: "Drain · \(hostname)") : String(localized: "Maintenance · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if kube { action = .none }
            await loadPlan()
        }
    }

    /// Another maintenance or an upgrade runs: the cluster lock would refuse this one anyway.
    private var busy: Bool { job.isActive || UpgradeJob.shared.isActive }

    private func canRequest(_ plan: MaintenancePlan) -> Bool {
        !busy && plan.canStart(action: action, acknowledged: Set(plan.acknowledgments(for: action)))
    }

    private func form(_ plan: MaintenancePlan) -> some View {
        let groups = plan.podGroups
        return Form {
            if let other = job.target, job.isActive {
                Section {
                    Text("A maintenance of \(other.hostname) is in progress. The app runs one at a time.")
                        .foregroundStyle(.statusWarn)
                }
            } else if UpgradeJob.shared.isActive {
                Section {
                    Text("An upgrade is in progress: wait until it ends.").foregroundStyle(.statusWarn)
                }
            }
            Section {
                if !kube {
                    LabeledContent("Role", value: plan.controlPlane ? String(localized: "Control plane") : String(localized: "Worker"))
                }
                LabeledContent("Kubernetes node", value: plan.kubeNode.or("—"))
                LabeledContent("Scheduling") {
                    Text(plan.cordoned ? String(localized: "Cordoned") : String(localized: "Schedulable"))
                        .foregroundStyle(plan.cordoned ? Color.statusWarn : Color.secondary)
                }
            }
            if !kube { actionSection }
            podsSection(String(localized: "Evicted (\(groups.toEvict.count))"), pods: groups.toEvict,
                        footer: String(localized: "Evictions honour PodDisruptionBudgets: a budget that allows no disruption makes the drain wait, it never forces."))
            if !groups.bare.isEmpty { bareSection(groups.bare) }
            podsSection(String(localized: "Left on the node (\(groups.leftAlone.count))"), pods: groups.leftAlone,
                        footer: String(localized: "DaemonSet and static pods stay: they would be recreated on the node."))
            checksSection(plan)
            Section {
                Button(role: .destructive) { Task { await requestStart() } } label: { Text(startTitle) }
                    .disabled(!canRequest(plan))
                if let message { Text(message).font(.footnote).foregroundStyle(.statusBad) }
            } footer: {
                Text("Stopping, or a failure, leaves the node cordoned. Keep Ichor open until the maintenance ends.")
            }
        }
        .themedBackground()
        .sheet(isPresented: $confirming) {
            HostnameConfirmationSheet(
                title: kube ? String(localized: "Drain \(hostname)?") : String(localized: "Maintenance of \(hostname)?"),
                message: confirmationMessage(plan),
                hostname: hostname,
                actionTitle: startTitle,
                acknowledgments: plan.acknowledgments(for: action)
            ) {
                confirming = false
                start(plan)
            }
        }
    }

    private var actionSection: some View {
        Section("After the drain") {
            Picker("Action", selection: $action) {
                ForEach(MaintenanceAction.allCases) { action in
                    VStack(alignment: .leading) {
                        Text(action.localizedLabel)
                        Text(action.localizedDetails).font(.caption).foregroundStyle(.secondary)
                    }
                    .tag(action)
                }
            }
            .pickerStyle(.inline)
            .labelsHidden()
        }
    }

    @ViewBuilder private func podsSection(_ title: String, pods: [DrainPod], footer: String) -> some View {
        Section {
            if pods.isEmpty {
                Text("None").foregroundStyle(.secondary)
            }
            ForEach(pods) { DrainPodRow(pod: $0) }
        } header: {
            Text(title)
        } footer: {
            Text(footer)
        }
    }

    private func bareSection(_ pods: [DrainPod]) -> some View {
        Section {
            ForEach(pods) { DrainPodRow(pod: $0) }
            Toggle("Also evict pods without a controller", isOn: $includeBare)
        } header: {
            Text("Without a controller (\(pods.count))")
        } footer: {
            Text("Nothing recreates them once evicted. Left alone, they keep the node from being fully drained.")
        }
    }

    @ViewBuilder private func checksSection(_ plan: MaintenancePlan) -> some View {
        if !plan.blockers.isEmpty || !plan.warnings.isEmpty || !plan.acknowledge.isEmpty {
            Section {
                ForEach(plan.blockers, id: \.self) { blocker in
                    Label { Text(verbatim: blocker) } icon: { Image(systemName: "xmark.octagon.fill") }
                        .foregroundStyle(action.takesNodeDown ? Color.statusBad : Color.secondary)
                }
                ForEach(plan.warnings, id: \.self) { warning in
                    Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                        .foregroundStyle(.statusWarn)
                }
                // Confirmed one by one in the confirmation sheet.
                ForEach(plan.acknowledgments(for: action), id: \.self) { risk in
                    Label { Text(verbatim: risk) } icon: { Image(systemName: "hand.raised.fill") }
                        .foregroundStyle(.statusWarn)
                }
            } header: {
                Text("Checks")
            } footer: {
                if !plan.blockers.isEmpty || !plan.acknowledge.isEmpty {
                    Text("These checks concern the reboot or shutdown: a drain alone runs anyway.")
                }
            }
        }
    }

    private var startTitle: String {
        switch action {
        case .reboot: String(localized: "Drain and reboot")
        case .shutdown: String(localized: "Drain and shut down")
        case .none: String(localized: "Drain")
        }
    }

    private func confirmationMessage(_ plan: MaintenancePlan) -> String {
        var text = switch action {
        case .reboot: String(localized: "\(hostname) is cordoned, its pods are evicted, then it reboots and is uncordoned once Ready again.")
        case .shutdown: String(localized: "\(hostname) is cordoned, its pods are evicted, then it shuts down. It stays cordoned and off until someone powers it on.")
        case .none: String(localized: "\(hostname) is cordoned and its pods are evicted. It stays cordoned: uncordon it from its menu when ready.")
        }
        if plan.controlPlane && action.takesNodeDown {
            text += "\n\n" + String(localized: "Control-plane node: it leaves etcd while down. Make sure the other members are healthy, or the cluster can lose quorum.")
        }
        text += "\n\n" + String(localized: "Keep Ichor open until the maintenance ends: in the background iOS pauses the app, and the maintenance with it.")
        return text
    }

    private func loadPlan() async {
        guard let client = model.client else { return }
        if kube {
            plan = await .from { try await client.kubeDrainPlan(kubeNode: node) }
        } else {
            plan = await .from { try await client.maintenancePlan(node: node) }
        }
    }

    /// App lock first (like reboot), then the typed hostname.
    private func requestStart() async {
        message = nil
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Maintenance of \(hostname)")) {
            message = failure
            return
        }
        confirming = true
    }

    /// After the confirmation sheet, which collected the acknowledgments.
    private func start(_ plan: MaintenancePlan) {
        let acknowledgments = plan.acknowledgments(for: action)
        guard let client = model.client, !busy, plan.canStart(action: action, acknowledged: Set(acknowledgments)) else { return }
        job.start(client: client, target: MaintenanceJob.Target(node: node, hostname: hostname, action: action,
                                                                wasCordoned: plan.cordoned, kube: kube),
                  includeBare: includeBare, acknowledged: !acknowledgments.isEmpty)
    }
}

/// A pod of the plan or the run: owner, budget, local data, and its state during the drain.
struct DrainPodRow: View {
    let pod: DrainPod

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            if let state = pod.podState {
                stateIcon(state).frame(width: 20)
            }
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: pod.name).font(.callout.monospaced())
                Text(verbatim: pod.owner.isEmpty ? pod.namespace : "\(pod.namespace) · \(pod.owner)")
                    .font(.caption).foregroundStyle(.secondary)
                if pod.hasPDB || pod.emptyDir {
                    HStack(spacing: 6) {
                        if pod.hasPDB {
                            StatusPill(label: String(localized: "PDB \(pod.pdb): \(pod.pdbAllowed) allowed"),
                                       color: pod.pdbBlocks ? .statusBad : .statusOK)
                        }
                        if pod.emptyDir { StatusPill(label: String(localized: "loses local data"), color: .statusWarn) }
                    }
                }
                if !pod.reason.isEmpty {
                    Text(verbatim: pod.reason).font(.caption).foregroundStyle(.statusWarn)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private func stateIcon(_ state: DrainPod.State) -> some View {
        switch state {
        case .pending: Image(systemName: "circle").foregroundStyle(.secondary).accessibilityLabel(Text("Pending"))
        case .evicting: ProgressView().accessibilityLabel(Text("Evicting"))
        case .blocked: Image(systemName: "pause.circle.fill").foregroundStyle(.statusWarn).accessibilityLabel(Text("Blocked"))
        case .gone: Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK).accessibilityLabel(Text("Evicted"))
        }
    }
}

extension MaintenanceAction {
    var localizedLabel: String {
        switch self {
        case .reboot: String(localized: "Reboot")
        case .shutdown: String(localized: "Shut down")
        case .none: String(localized: "Drain only")
        }
    }

    var localizedDetails: String {
        switch self {
        case .reboot: String(localized: "Reboots, waits until the node is Ready again, then uncordons it.")
        case .shutdown: String(localized: "Powers the node off; it stays cordoned.")
        case .none: String(localized: "Stops after the drain; the node stays cordoned.")
        }
    }
}
