import SwiftUI
import TalosdevMobileCore

struct EtcdView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<EtcdOverview> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { etcd in
            let hostnames = Dictionary(etcd.members.map { ($0.id, $0.hostname) }, uniquingKeysWith: { first, _ in first })
            List {
                if let error = etcd.error { Text(error).foregroundStyle(.red) }
                if !etcd.alarms.isEmpty {
                    Section("Alarms") {
                        ForEach(etcd.alarms, id: \.self) { Text("\(hostnames[$0.memberId] ?? $0.memberId): \($0.alarm)").foregroundStyle(.red) }
                    }
                }
                Section("Members (\(etcd.members.count))") {
                    ForEach(etcd.statuses) { status in
                        MemberStatusRow(status: status, hostname: hostnames[status.memberId] ?? status.node)
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle("etcd")
        .task { await load() }
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
