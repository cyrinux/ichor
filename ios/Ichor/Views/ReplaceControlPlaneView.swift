import SwiftUI
import UIKit
import IchorCore

/// The guided replacement of a failed control plane: quorum check, removal of its etcd
/// member, reset (or power-off) of the old node, the new node booted by hand from a template
/// config, then the wait for the new member. Each action keeps its own confirmation; the
/// steps are read from the cluster, so leaving and coming back resumes.
struct ReplaceControlPlaneView: View {
    let memberId: String
    let hostname: String

    @Environment(AppModel.self) private var model
    @State private var node: String
    @State private var state: LoadState<CpReplacePlan> = .loading
    /// The voting members to wait past, from the first plan read.
    @State private var membersBefore: Int?
    @State private var removing = false
    @State private var resetting = false
    @State private var resetRunning = false
    @State private var message: String?
    @State private var join: Join = .idle
    @State private var waitTask: Task<Void, Never>?

    enum Join: Equatable {
        case idle
        case waiting(String)
        case joined
        case failed(String)
    }

    init(memberId: String, node: String, hostname: String) {
        self.memberId = memberId
        self.hostname = hostname
        _node = State(initialValue: node)
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { plan in
            List {
                header(plan)
                if let message {
                    Section { Text(verbatim: message).font(.footnote) }
                }
                step(plan, .confirmQuorum, number: 1, title: "Check etcd quorum") {
                    LabeledContent("Members after", value: "\(plan.quorum.afterRemoval)")
                    LabeledContent("Healthy after", value: "\(plan.quorum.healthyAfter)")
                }
                step(plan, .removeMember, number: 2, title: "Remove the etcd member") {
                    if plan.state(.removeMember) == .ready {
                        Button("Remove member…", role: .destructive) { removing = true }
                    }
                }
                step(plan, .resetOrPowerOff, number: 3, title: "Reset the old node") { resetRow(plan) }
                step(plan, .bootNewNode, number: 4, title: "Boot the new node") { bootRows(plan) }
                step(plan, .waitMember, number: 5, title: "Wait for the new member", done: join == .joined) { waitRows(plan) }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle("Replace a control plane")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .onDisappear { waitTask?.cancel() }
        .sheet(isPresented: $removing) { removeSheet }
        .sheet(isPresented: $resetting) { resetSheet }
    }

    private func header(_ plan: CpReplacePlan) -> some View {
        Section {
            Text(verbatim: plan.member.hostname.isEmpty ? (hostname.isEmpty ? plan.member.id : hostname) : plan.member.hostname)
                .font(.headline)
            if !plan.member.node.isEmpty {
                Text(verbatim: plan.member.node).font(.caption.monospaced()).foregroundStyle(.secondary)
            }
            LabeledContent("Member ID") { Text(verbatim: plan.member.id).font(.caption.monospaced()) }
        } footer: {
            Text("Each step asks for its own confirmation. The steps are read from the cluster: leave and come back at any time.")
        }
    }

    private func step<Content: View>(_ plan: CpReplacePlan, _ step: CpStep, number: Int, title: LocalizedStringKey, done: Bool = false,
                                     @ViewBuilder content: () -> Content) -> some View {
        let state = done ? CpStepState.done : plan.state(step)
        let detail = plan.step(step).detail
        return Section {
            HStack {
                Text(verbatim: "\(number).")
                Text(title).fontWeight(.semibold)
                Spacer()
                StepStateBadge(state: state)
            }
            if !detail.isEmpty {
                Text(verbatim: detail).font(.footnote).foregroundStyle(state == .blocked ? Color.statusBad : Color.secondary)
            }
            content()
        }
    }

    @ViewBuilder
    private func resetRow(_ plan: CpReplacePlan) -> some View {
        if resetRunning {
            HStack { ProgressView(); Text("Working…") }
        } else if plan.state(.resetOrPowerOff) == .skipped {
            Text("The old node does not answer, so it cannot be reset from here: power it off yourself if it still runs.")
                .font(.footnote).foregroundStyle(.secondary)
        } else if plan.state(.resetOrPowerOff) == .ready {
            Button("Reset…", role: .destructive) { resetting = true }
        }
    }

    @ViewBuilder
    private func bootRows(_ plan: CpReplacePlan) -> some View {
        if !plan.template.node.isEmpty {
            let name = plan.template.hostname.isEmpty ? plan.template.node : plan.template.hostname
            Text(String(localized: "Boot the new machine from a Talos image (maintenance mode), then apply the machine config of \(name) to it from a laptop. Joining a node from the phone comes later."))
                .font(.footnote).foregroundStyle(.secondary)
            Text(verbatim: applyConfigCommand).font(.caption.monospaced()).textSelection(.enabled)
            NavigationLink {
                MachineConfigView(node: plan.template.node, hostname: name)
            } label: {
                Text("Open the template config")
            }
            Button("Copy the command") { UIPasteboard.general.string = applyConfigCommand }
        }
    }

    @ViewBuilder
    private func waitRows(_ plan: CpReplacePlan) -> some View {
        switch join {
        case .joined:
            Text("The new member joined: etcd is healthy.").foregroundStyle(.statusOK)
        case .waiting(let detail):
            HStack { ProgressView(); Text("Waiting for a new healthy etcd member…") }
            if !detail.isEmpty { Text(verbatim: detail).font(.footnote).foregroundStyle(.secondary) }
            Button("Stop waiting") { stopWait() }
        case .failed(let error):
            Text(verbatim: error).font(.footnote).foregroundStyle(.statusBad)
            Button("Retry") { startWait() }
        case .idle:
            if plan.state(.waitMember) == .ready {
                Button("Start waiting") { startWait() }
            }
        }
    }

    @ViewBuilder
    private var removeSheet: some View {
        if let client = model.client, case .loaded(let plan, _, _) = state {
            EtcdRemoveMemberSheet(member: EtcdMember(id: plan.member.id, hostname: plan.member.hostname),
                                  throughNode: plan.template.node.isEmpty ? nil : plan.template.node,
                                  client: client, lockEnabled: model.lock.enabled) { name in
                removing = false
                message = String(localized: "\(name) was removed from etcd")
                Task { await load() }
            }
        }
    }

    @ViewBuilder
    private var resetSheet: some View {
        if case .loaded(let plan, _, _) = state {
            let name = plan.member.hostname.isEmpty ? node : plan.member.hostname
            // The member already left etcd: a graceful reset would try to leave it again.
            NodeResetSheet(node: node, hostname: name, initialGraceful: false) { request in
                resetting = false
                Task { await performReset(request, name: name) }
            }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let next = await LoadState.from { try await client.controlPlaneReplacePlan(memberID: memberId, node: node) }
        if case .loaded(let plan, _, _) = next {
            if !plan.member.node.isEmpty { node = plan.member.node }
            if membersBefore == nil { membersBefore = plan.membersBeforeJoin }
        }
        state = state.refreshed(with: next)
    }

    private func performReset(_ request: NodeResetSheet.Request, name: String) async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Reset \(name)")) {
            message = failure
            return
        }
        resetRunning = true
        defer { resetRunning = false }
        do {
            try await client.reset(node: node, wipe: request.wipe, graceful: request.graceful, reboot: request.reboot)
            message = String(localized: "\(name): reset requested")
        } catch {
            message = error.localizedDescription
        }
        await load()
    }

    /// Polls etcd in 30-second slices until the new member is a healthy voter; read-only,
    /// cancelled when the screen goes away.
    private func startWait() {
        guard let client = model.client, let before = membersBefore, waitTask == nil else { return }
        join = .waiting("")
        waitTask = Task {
            defer { waitTask = nil }
            while !Task.isCancelled {
                do {
                    let result = try await client.controlPlaneReplaceWait(membersBefore: before, timeout: 30)
                    if Task.isCancelled { return }
                    if result.joined {
                        join = .joined
                        await load()
                        return
                    }
                    join = .waiting(result.detail)
                } catch {
                    if !Task.isCancelled { join = .failed(error.localizedDescription) }
                    return
                }
            }
        }
    }

    private func stopWait() {
        waitTask?.cancel()
        waitTask = nil
        join = .idle
    }
}

/// A step's state as a colored capsule.
private struct StepStateBadge: View {
    let state: CpStepState

    var body: some View {
        Text(label)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 2)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: Capsule())
    }

    private var label: LocalizedStringKey {
        switch state {
        case .pending: "Pending"
        case .ready: "To do"
        case .done: "Done"
        case .skipped: "Skipped"
        case .blocked: "Blocked"
        }
    }

    private var color: Color {
        switch state {
        case .done: .statusOK
        case .ready: .statusWarn
        case .blocked: .statusBad
        case .pending, .skipped: .secondary
        }
    }
}
