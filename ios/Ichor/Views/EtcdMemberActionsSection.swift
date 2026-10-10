import SwiftUI
import IchorCore

/// "Forfeit leadership" and "Remove member…" of the etcd screen (os:admin). Shown disabled
/// with the version notice when no node's Talos has them.
struct EtcdMemberActionsSection: View {
    let etcd: EtcdOverview
    let support: FeatureSupport
    let busy: Bool
    let message: String?
    let onForfeit: (EtcdNodeStatus) -> Void
    let onRemove: (EtcdMember) -> Void

    private var leader: EtcdNodeStatus? {
        etcd.statuses.first { status in
            status.error == nil && etcdMemberActions(memberId: status.memberId, etcd: etcd).forfeitNode != nil
        }
    }

    private var removable: [EtcdMember] {
        etcd.members.filter { etcdMemberActions(memberId: $0.id, etcd: etcd).canRemove }
    }

    var body: some View {
        Section {
            if busy {
                HStack { ProgressView(); Text("Working…") }
            } else {
                Button("Forfeit leadership") { if let leader { onForfeit(leader) } }
                    .disabled(leader == nil || !support.supported)
                Menu {
                    ForEach(removable) { member in
                        Button(role: .destructive) { onRemove(member) } label: {
                            Text(verbatim: member.hostname.isEmpty ? member.id : member.hostname)
                        }
                    }
                } label: {
                    Text("Remove member…").foregroundStyle(removable.isEmpty || !support.supported ? Color.secondary : Color.red)
                }
                .disabled(removable.isEmpty || !support.supported)
                // The guided replacement, for a failed member only.
                ForEach(replaceCandidates(etcd)) { member in
                    NavigationLink(value: Route.replaceControlPlane(memberId: member.id, node: etcdMemberAddress(member),
                                                                    hostname: member.hostname)) {
                        VStack(alignment: .leading) {
                            Text("Replace this control plane…")
                            Text(verbatim: member.hostname.isEmpty ? member.id : member.hostname)
                                .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .disabled(!support.supported)
                }
            }
            if let notice = support.localizedNotice { VersionNoticeRow(text: notice) }
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        } header: {
            Text("Member actions")
        } footer: {
            Text("Forfeiting moves etcd leadership to another member. Removing a member is for a node that is gone or being replaced: what the removal would leave is checked first.")
        }
    }
}

/// The plan of a member removal: what is left, what forbids it, then the confirmation (Face
/// ID / passcode when the app lock is on, and typing the member's hostname).
struct EtcdRemoveMemberSheet: View {
    let member: EtcdMember
    /// The node the removal is sent to; nil when no other member is reachable.
    let throughNode: String?
    let client: TalosClient
    let lockEnabled: Bool
    /// Called after a successful removal with the name of the removed member.
    let onRemoved: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var plan: LoadState<EtcdMemberPlan> = .loading
    @State private var confirming = false
    @State private var removing = false
    @State private var message: String?

    // Explicit: the private @State properties make the memberwise init private.
    init(member: EtcdMember, throughNode: String?, client: TalosClient, lockEnabled: Bool, onRemoved: @escaping (String) -> Void) {
        self.member = member
        self.throughNode = throughNode
        self.client = client
        self.lockEnabled = lockEnabled
        self.onRemoved = onRemoved
    }

    var body: some View {
        NavigationStack {
            LoadStateView(state: plan, retry: load) { plan in form(plan) }
                .navigationTitle("Remove member")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(removing) }
                }
        }
        .interactiveDismissDisabled(removing)
        .task { await load() }
    }

    private func form(_ plan: EtcdMemberPlan) -> some View {
        let gate = EtcdRemovalGate(plan: plan, busy: removing)
        return Form {
            Section {
                LabeledContent("Member", value: gate.confirmationName)
                LabeledContent("Member ID") { Text(verbatim: plan.member.id).font(.caption.monospaced()).textSelection(.enabled) }
            }
            Section {
                LabeledContent("Members after", value: "\(plan.membersAfter)")
                LabeledContent("Healthy after", value: "\(plan.healthyAfter)")
                LabeledContent("Quorum") {
                    Text(plan.keepsQuorum ? String(localized: "Kept") : String(localized: "Lost"))
                        .foregroundStyle(plan.keepsQuorum ? Color.green : Color.red)
                }
            }
            if !plan.blockers.isEmpty || !plan.warnings.isEmpty || throughNode == nil {
                Section("Checks") {
                    ForEach(plan.blockers, id: \.self) { blocker in
                        Label { Text(verbatim: blocker) } icon: { Image(systemName: "xmark.octagon.fill") }
                            .foregroundStyle(.statusBad)
                    }
                    if throughNode == nil {
                        Label("No other reachable member can take the request.", systemImage: "xmark.octagon.fill")
                            .foregroundStyle(.statusBad)
                    }
                    ForEach(plan.warnings, id: \.self) { warning in
                        Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                            .foregroundStyle(.statusWarn)
                    }
                }
            }
            Section {
                if removing {
                    HStack { ProgressView(); Text("Removing…") }
                } else {
                    Button(role: .destructive) { Task { await requestRemoval() } } label: { Text("Remove member…") }
                        .disabled(!gate.canRemove || throughNode == nil)
                }
                if let message { Text(message).font(.footnote).foregroundStyle(.statusBad) }
            } footer: {
                Text("The member leaves the etcd cluster. This cannot be undone: the node must be reset or rejoin to become a member again.")
            }
        }
        .sheet(isPresented: $confirming) {
            HostnameConfirmationSheet(
                title: String(localized: "Remove \(gate.confirmationName) from etcd?"),
                message: String(localized: "The member leaves the etcd cluster. This cannot be undone: the node must be reset or rejoin to become a member again."),
                hostname: gate.confirmationName,
                actionTitle: String(localized: "Remove member")
            ) {
                confirming = false
                Task { await remove(plan) }
            }
        }
    }

    private func load() async {
        plan = await .from { try await client.etcdMemberPlan(memberID: member.id) }
    }

    private func requestRemoval() async {
        message = nil
        if lockEnabled, let failure = await Authenticator.authenticate(reason: String(localized: "Remove etcd member \(member.hostname.isEmpty ? member.id : member.hostname)")) {
            message = failure
            return
        }
        confirming = true
    }

    private func remove(_ plan: EtcdMemberPlan) async {
        let gate = EtcdRemovalGate(plan: plan, busy: removing)
        guard gate.canRemove, let throughNode else { return }
        removing = true
        defer { removing = false }
        do {
            try await client.etcdRemoveMember(node: throughNode, memberID: plan.member.id)
            onRemoved(gate.confirmationName)
        } catch {
            message = error.localizedDescription
            await load() // the plan may have changed
        }
    }
}
