import SwiftUI
import IchorCore

struct EtcdView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<EtcdOverview> = .loading
    @State private var confirm: [EtcdNodeStatus] = []
    @State private var progress: String?
    @State private var result: String?
    @State private var snapshot = EtcdSnapshotJob()
    @State private var confirmDisarm = false
    @State private var disarming = false
    @State private var alarmMessage: String?
    @State private var forfeit: EtcdNodeStatus?
    @State private var removing: EtcdMember?
    @State private var memberBusy = false
    @State private var memberMessage: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { etcd in
            let hostnames = Dictionary(etcd.members.map { ($0.id, $0.hostname) }, uniquingKeysWith: { first, _ in first })
            List {
                if let error = etcd.error { ErrorOrNoticeText(message: error) }
                if model.allows(.etcdDefrag) {
                    let order = defragOrder(etcd.statuses)
                    let reclaimable = order.reduce(Int64(0)) { $0 + $1.reclaimable }
                    Section {
                        if let progress {
                            HStack { ProgressView(); Text(progress) }
                        } else {
                            if let result { Text(result).font(.footnote) }
                            Text(String(localized: "\(order.count) members to defragment.") + " "
                                + String(localized: "About \(formatBytes(reclaimable)) reclaimable. Members are done one at a time, followers first and the leader last."))
                                .font(.footnote).foregroundStyle(.secondary)
                            Button("Defragment all") { confirm = order }
                                .disabled(order.isEmpty || reclaimable == 0)
                        }
                    } header: {
                        Text("Defragmentation")
                    }
                }
                if model.allows(.etcdSnapshot) {
                    EtcdSnapshotSection(etcd: etcd, hostnames: hostnames, job: snapshot)
                }
                if !etcd.alarms.isEmpty || alarmMessage != nil || etcd.alarmsError != nil {
                    Section("Alarms") {
                        if let alarmsError = etcd.alarmsError { Text("Could not check alarms: \(alarmsError)").foregroundStyle(.red) }
                        ForEach(etcd.alarms, id: \.self) { Text(verbatim: "\(hostnames[$0.memberId] ?? $0.memberId): \($0.alarm)").foregroundStyle(.red) }
                        if let alarmMessage { Text(alarmMessage).font(.footnote).foregroundStyle(.secondary) }
                        if model.allows(.etcdDefrag) && !etcd.alarms.isEmpty {
                            if disarming {
                                HStack { ProgressView(); Text("Disarming…") }
                            } else {
                                Button("Disarm alarms", role: .destructive) { confirmDisarm = true }
                                    .disabled(disarmNode(etcd) == nil)
                            }
                        }
                    }
                }
                if model.allows(.etcdMemberActions) {
                    EtcdMemberActionsSection(etcd: etcd, support: model.clusterSupport(.etcdMemberActions),
                                             busy: memberBusy, message: memberMessage,
                                             onForfeit: { forfeit = $0 }, onRemove: { removing = $0 })
                }
                Section("Members (\(etcd.members.count))") {
                    ForEach(etcd.statuses) { status in
                        MemberStatusRow(status: status, hostname: hostnames[status.memberId] ?? status.node)
                            .swipeActions {
                                if model.allows(.etcdDefrag) && progress == nil && status.error == nil {
                                    Button("Defragment") { confirm = [status] }.tint(.orange)
                                }
                            }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(Text(verbatim: "etcd"))
        .task { await load() }
        .confirmationDialog(Text("Disarm etcd alarms?"), isPresented: $confirmDisarm, titleVisibility: .visible) {
            Button("Disarm alarms", role: .destructive) { Task { await disarm() } }
        } message: {
            Text("Disarming does not fix the cause. For a NOSPACE alarm, free disk space and defragment first, or the alarm comes back. Once disarmed, etcd accepts writes again.")
        }
        .confirmationDialog(Text("Forfeit leadership?"),
                            isPresented: Binding(get: { forfeit != nil }, set: { if !$0 { forfeit = nil } }),
                            titleVisibility: .visible, presenting: forfeit) { leader in
            Button("Forfeit leadership", role: .destructive) { Task { await forfeitLeadership(leader) } }
        } message: { leader in
            Text("\(memberName(leader)) gives up etcd leadership and another member is elected. Writes pause briefly during the election.")
        }
        .sheet(item: $removing) { member in
            if let client = model.client, case .loaded(let etcd, _, _) = state {
                EtcdRemoveMemberSheet(member: member, throughNode: etcdRemovalNode(memberId: member.id, statuses: etcd.statuses),
                                      client: client, lockEnabled: model.lock.enabled) { name in
                    removing = nil
                    memberMessage = String(localized: "\(name) was removed from etcd")
                    Task { await load() }
                }
            }
        }
        .fileMover(isPresented: Binding(get: { snapshot.moving }, set: { snapshot.moving = $0 }), file: snapshot.file,
                   onCompletion: { snapshot.moved($0) }, onCancellation: { snapshot.moveCancelled() })
        .onDisappear { snapshot.leave() }
        .confirmationDialog(
            confirm.count == 1 ? Text("Defragment this member?") : Text("Defragment \(confirm.count) members?"),
            isPresented: Binding(get: { !confirm.isEmpty }, set: { if !$0 { confirm = [] } }),
            titleVisibility: .visible
        ) {
            Button("Defragment") {
                let targets = confirm
                confirm = []
                Task { await defragment(targets) }
            }
        } message: {
            Text("Each member is busy while it is defragmented. Members are done one at a time, never together, so the cluster keeps quorum.")
        }
    }

    /// One member at a time, stopping at the first failure; Face ID first when the lock is on.
    private func defragment(_ targets: [EtcdNodeStatus]) async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Defragment etcd")) {
            result = failure
            return
        }
        let names: [String: String]
        if case .loaded(let etcd, _, _) = state {
            names = Dictionary(etcd.members.map { ($0.id, $0.hostname) }, uniquingKeysWith: { a, _ in a })
        } else {
            names = [:]
        }
        for (index, member) in targets.enumerated() {
            let name = names[member.memberId] ?? member.node
            progress = String(localized: "Defragmenting \(name) (\(index + 1)/\(targets.count))…")
            do {
                try await client.defragment(node: member.node)
            } catch {
                progress = nil
                result = "\(name): \(error.localizedDescription)"
                await load()
                return
            }
        }
        progress = nil
        let reclaimed = formatBytes(targets.reduce(Int64(0)) { $0 + $1.reclaimable })
        result = String(localized: "Defragmented \(targets.count) members.") + " " + String(localized: "About \(reclaimed) reclaimed.")
        await load()
    }

    private func load() async {
        guard let client = model.client else { return }
        state = model.seeded(state, from: .etcd)
        state = state.refreshed(with: await .from { try await model.fetch(.etcd, with: client) })
        // Which Talos versions the members run decides whether the member actions exist.
        if model.allows(.etcdMemberActions), case .loaded(let etcd, _, _) = state {
            for status in etcd.statuses where status.error == nil { await model.loadFeatures(node: status.node) }
        }
    }

    /// The hostname of a status' member, its node address when the member list does not have it.
    private func memberName(_ status: EtcdNodeStatus) -> String {
        guard case .loaded(let etcd, _, _) = state,
              let member = etcd.members.first(where: { $0.id == status.memberId }), !member.hostname.isEmpty else { return status.node }
        return member.hostname
    }

    /// Face ID first when the lock is on, then the leader steps down and the list reloads.
    private func forfeitLeadership(_ leader: EtcdNodeStatus) async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Forfeit etcd leadership")) {
            memberMessage = failure
            return
        }
        memberBusy = true
        defer { memberBusy = false }
        do {
            let result = try await client.etcdForfeitLeadership(node: leader.node)
            // No new leader named: the node was not the leader (any more) and nothing changed.
            memberMessage = result.member.isEmpty
                ? String(localized: "\(memberName(leader)) was not the leader; nothing changed")
                : String(localized: "\(memberName(leader)) gave up leadership")
        } catch {
            memberMessage = error.localizedDescription
        }
        await load()
    }

    /// Alarms are cluster-wide; any reachable member can disarm them.
    private func disarmNode(_ etcd: EtcdOverview) -> String? {
        defaultSnapshotMember(etcd.statuses)?.node ?? etcd.statuses.first { $0.error == nil }?.node
    }

    private func disarm() async {
        guard let client = model.client, case .loaded(let etcd, _, _) = state, let node = disarmNode(etcd) else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Disarm etcd alarms")) {
            alarmMessage = failure
            return
        }
        disarming = true
        defer { disarming = false }
        do {
            try await client.disarmEtcdAlarms(node: node)
            alarmMessage = String(localized: "Alarms disarmed.")
        } catch {
            alarmMessage = error.localizedDescription
        }
        await load()
    }
}

private struct MemberStatusRow: View {
    let status: EtcdNodeStatus
    let hostname: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                VStack(alignment: .leading) {
                    Text(hostname).font(.headline)
                    Text(status.node).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                pill
            }
            if let error = status.error {
                Text(error).font(.caption).foregroundStyle(.red)
            } else {
                LabeledContent("Member ID", value: status.memberId).font(.caption.monospaced())
                LabeledContent("DB size", value: String(localized: "\(formatBytes(status.dbSizeInUse)) in use / \(formatBytes(status.dbSize))")).font(.caption)
                UsageBar(fraction: status.dbSize > 0 ? Double(status.dbSizeInUse) / Double(status.dbSize) : 0)
                LabeledContent("Raft term / index", value: "\(status.raftTerm) / \(status.raftIndex)").font(.caption)
                ForEach(status.errors, id: \.self) { Text($0).font(.caption).foregroundStyle(.red) }
            }
        }
    }

    @ViewBuilder private var pill: some View {
        if status.error != nil || !status.errors.isEmpty {
            StatusPill(label: String(localized: "Error"), color: .red)
        } else if status.isLeader {
            StatusPill(label: String(localized: "Leader"), color: .green)
        } else if status.isLearner {
            StatusPill(label: String(localized: "Learner"), color: .orange)
        } else {
            StatusPill(label: String(localized: "Follower"), color: .gray)
        }
    }
}
