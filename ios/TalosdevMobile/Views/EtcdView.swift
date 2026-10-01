import SwiftUI
import TalosdevMobileCore

struct EtcdView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<EtcdOverview> = .loading
    @State private var confirm: [EtcdNodeStatus] = []
    @State private var progress: String?
    @State private var result: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { etcd in
            let hostnames = Dictionary(etcd.members.map { ($0.id, $0.hostname) }, uniquingKeysWith: { first, _ in first })
            List {
                if let error = etcd.error { Text(error).foregroundStyle(.red) }
                if model.allows(.etcdDefrag) {
                    let order = defragOrder(etcd.statuses)
                    let reclaimable = order.reduce(Int64(0)) { $0 + $1.reclaimable }
                    Section {
                        if let progress {
                            HStack { ProgressView(); Text(progress) }
                        } else {
                            if let result { Text(result).font(.footnote) }
                            Text("About \(formatBytes(reclaimable)) reclaimable across \(order.count) member(s). Members are done one at a time, followers first and the leader last.")
                                .font(.footnote).foregroundStyle(.secondary)
                            Button("Defragment all") { confirm = order }
                                .disabled(order.isEmpty || reclaimable == 0)
                        }
                    } header: {
                        Text("Defragmentation")
                    }
                }
                if !etcd.alarms.isEmpty {
                    Section("Alarms") {
                        ForEach(etcd.alarms, id: \.self) { Text("\(hostnames[$0.memberId] ?? $0.memberId): \($0.alarm)").foregroundStyle(.red) }
                    }
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
        .navigationTitle("etcd")
        .task { await load() }
        .confirmationDialog(
            confirm.count == 1 ? "Defragment this member?" : "Defragment \(confirm.count) members?",
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
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: "Defragment etcd") {
            result = failure
            return
        }
        let names: [String: String]
        if case .loaded(let etcd) = state {
            names = Dictionary(etcd.members.map { ($0.id, $0.hostname) }, uniquingKeysWith: { a, _ in a })
        } else {
            names = [:]
        }
        for (index, member) in targets.enumerated() {
            let name = names[member.memberId] ?? member.node
            progress = "Defragmenting \(name) (\(index + 1)/\(targets.count))…"
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
        result = "Defragmented \(targets.count) member(s), about \(formatBytes(targets.reduce(Int64(0)) { $0 + $1.reclaimable })) reclaimed."
        await load()
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.etcd() }
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
                LabeledContent("DB size", value: "\(formatBytes(status.dbSizeInUse)) in use / \(formatBytes(status.dbSize))").font(.caption)
                UsageBar(fraction: status.dbSize > 0 ? Double(status.dbSizeInUse) / Double(status.dbSize) : 0)
                LabeledContent("Raft term / index", value: "\(status.raftTerm) / \(status.raftIndex)").font(.caption)
                ForEach(status.errors, id: \.self) { Text($0).font(.caption).foregroundStyle(.red) }
            }
        }
    }

    @ViewBuilder private var pill: some View {
        if status.error != nil || !status.errors.isEmpty {
            StatusPill(label: "Error", color: .red)
        } else if status.isLeader {
            StatusPill(label: "Leader", color: .green)
        } else if status.isLearner {
            StatusPill(label: "Learner", color: .orange)
        } else {
            StatusPill(label: "Follower", color: .gray)
        }
    }
}
