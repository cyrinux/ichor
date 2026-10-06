import SwiftUI
import IchorCore

/// Every CloudNativePG cluster, built for dozens: a summary, the ones that need a look by default,
/// one compact row each that opens to the details, where a backup can be started now (os:admin).
struct CnpgList: View {
    let status: CnpgStatus
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var problemsOnly: Bool?
    @State private var query = ""
    @State private var confirmBackup: CnpgCluster?
    @State private var running: Set<String> = []
    @State private var resultMessage: String?

    var body: some View {
        let attention = status.clusters.filter(\.health.needsAttention).count
        let current = problemsOnly ?? (attention > 0)
        let rows = filterClusters(status.clusters, problemsOnly: current, query: query)
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: [String(localized: "\(status.clusters.count) Postgres clusters"),
                                    attention > 0 ? String(localized: "\(attention) need a look") : nil]
                        .compactMap { $0 }.joined(separator: " · "))
                        .font(.subheadline.weight(.semibold))
                    if status.pendingInstances > 0 {
                        Text("\(status.pendingInstances) instances waiting to be scheduled").font(.caption).foregroundStyle(attentionColor)
                    }
                }
                Picker(selection: Binding(get: { current }, set: { problemsOnly = $0 })) {
                    Text("Problems").tag(true)
                    Text("All").tag(false)
                } label: {
                    EmptyView()
                }
                .pickerStyle(.segmented)
            }
            Section {
                if rows.isEmpty {
                    Group {
                        if !query.isEmpty {
                            Text("Nothing matches “\(query)”.")
                        } else if current {
                            Text("Nothing needs attention.")
                        } else {
                            Text("No Postgres clusters.")
                        }
                    }
                    .foregroundStyle(.secondary)
                }
                ForEach(rows) { cluster in
                    ClusterRow(cluster: cluster, busy: running.contains(cluster.id)) { confirmBackup = cluster }
                }
            }
        }
        .searchable(text: $query, prompt: Text("Filter by namespace or name"))
        .refreshable { await refresh() }
        .themedBackground()
        .confirmationDialog(confirmBackup.map { String(localized: "Back up \($0.label) now?") } ?? "",
                            isPresented: $confirmBackup.isPresent(),
                            titleVisibility: .visible,
                            presenting: confirmBackup) { cluster in
            Button("Back up now") { Task { await backup(cluster) } }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("CloudNativePG takes a full base backup with the method the cluster is set up for (barman-cloud plugin, object store or volume snapshot). It loads the instance it runs on and adds to the backup storage.")
        }
        .messageAlert($resultMessage)
    }

    private func backup(_ cluster: CnpgCluster) async {
        guard let client = model.client, !running.contains(cluster.id) else { return }
        running.insert(cluster.id)
        defer { running.remove(cluster.id) }
        do {
            let name = try await client.cnpgBackup(namespace: cluster.namespace, name: cluster.name)
            resultMessage = String(localized: "Backup \(name) requested")
            await refresh()
        } catch {
            resultMessage = String(localized: "Could not back up \(cluster.label): \(error.localizedDescription)")
        }
    }
}

private struct ClusterRow: View {
    let cluster: CnpgCluster
    let busy: Bool
    let onBackup: () -> Void

    var body: some View {
        DisclosureGroup {
            details
        } label: {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: cluster.health).padding(.top, 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: cluster.label).font(.subheadline.monospaced()).lineLimit(1)
                    Text(verbatim: summary)
                        .font(.caption)
                        .foregroundStyle(cluster.health.needsAttention ? cluster.health.color : .secondary)
                        .lineLimit(2)
                }
            }
            .accessibilityElement(children: .combine)
        }
    }

    private var summary: String {
        var parts = [String(localized: "\(cluster.readyInstances)/\(cluster.instances) ready")]
        if cluster.hibernated { parts.append(String(localized: "hibernated")) }
        parts += cluster.reasons.map(reasonText)
        return parts.joined(separator: " · ")
    }

    private func reasonText(_ reason: CnpgReason) -> String {
        switch reason {
        case .noInstance: String(localized: "no instance running")
        case .failover: String(localized: "failing over")
        case .instances: String(localized: "instances missing")
        case .switchover: String(localized: "switchover in progress") + " (\(cluster.currentPrimary) → \(cluster.targetPrimary))"
        case .notReady: String(localized: "not ready")
        case .archiving: String(localized: "WAL archiving failing")
        case .backupFailed: String(localized: "last backup failed")
        case .backupStale: String(localized: "no recent backup")
        }
    }

    @ViewBuilder private var details: some View {
        if !cluster.phase.isEmpty {
            LabeledContent("Phase", value: [cluster.phase, cluster.phaseReason].filter { !$0.isEmpty }.joined(separator: ": "))
        }
        if !cluster.currentPrimary.isEmpty {
            let switching = !cluster.targetPrimary.isEmpty && cluster.targetPrimary != cluster.currentPrimary
            LabeledContent("Primary", value: switching ? "\(cluster.currentPrimary) → \(cluster.targetPrimary)" : cluster.currentPrimary)
        }
        if !cluster.instancePods.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text("Instances").font(.caption).foregroundStyle(.secondary)
                ForEach(cluster.instancePods) { pod in
                    HStack(spacing: 6) {
                        HealthDot(health: pod.ready ? .ok : .critical, size: 8)
                        Text(verbatim: pod.line).font(.caption.monospaced()).foregroundStyle(pod.ready ? .primary : Color.red)
                    }
                }
            }
        }
        LabeledContent("WAL archiving", value: archivingText)
        LabeledContent("Backups", value: backupMethod)
        LabeledContent("Last backup", value: relativeTime(cluster.lastSuccessAt))
        if cluster.lastFailureAt > 0 { LabeledContent("Last failure", value: relativeTime(cluster.lastFailureAt)) }
        if cluster.recoverableAt > 0 { LabeledContent("Oldest recovery point", value: relativeTime(cluster.recoverableAt)) }
        // The Go core picks the method; a cluster set up for none of them is refused with a reason.
        if !cluster.hibernated {
            Button(action: onBackup) {
                HStack {
                    Label("Back up now", systemImage: "icloud.and.arrow.up")
                    if busy { Spacer(); ProgressView() }
                }
            }
            .disabled(busy)
        }
    }

    private var archivingText: String {
        switch cluster.archiving {
        case "ok": String(localized: "working")
        case "failing": String(localized: "failing")
        case "off": String(localized: "off")
        default: String(localized: "unknown")
        }
    }

    private var backupMethod: String {
        switch cluster.backupMethod {
        case "plugin": String(localized: "barman-cloud plugin (\(cluster.objectStore.or("—")))")
        case "in-tree": String(localized: "barman object store")
        default: String(localized: "not configured")
        }
    }
}
