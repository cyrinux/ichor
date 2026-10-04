import SwiftUI
import IchorCore

/// Every MariaDB cluster (problems first) with its topology, pods and their role, and its backups.
struct MariaDbList: View {
    let status: MariaDbStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                if status.clusters.isEmpty {
                    Text("No MariaDB clusters.").foregroundStyle(.secondary)
                }
                ForEach(status.clusters) { ClusterRow(cluster: $0) }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

private struct ClusterRow: View {
    let cluster: MariaDbCluster

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 12) {
                HealthDot(health: cluster.health)
                Text(verbatim: cluster.label).font(.subheadline.monospaced()).lineLimit(1)
            }
            Text(verbatim: summary)
                .font(.caption)
                .foregroundStyle(cluster.health.needsAttention ? cluster.health.color : .secondary)
            ForEach(cluster.pods) { pod in
                HStack(spacing: 6) {
                    HealthDot(health: pod.ready ? .ok : .critical, size: 8)
                    Text(verbatim: podLine(pod)).font(.caption.monospaced()).foregroundStyle(pod.ready ? .primary : Color.red)
                }
            }
            if let backups {
                Text(verbatim: backups).font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private var summary: String {
        ([topology, String(localized: "\(cluster.readyPods)/\(cluster.replicas) ready"),
          cluster.suspended ? String(localized: "suspended") : nil].compactMap { $0 } + cluster.reasons.map(reasonText))
            .joined(separator: " · ")
    }

    private var topology: String? {
        switch cluster.topology {
        case "standalone": String(localized: "standalone")
        case "replication": String(localized: "replication")
        case "galera": "Galera"
        default: cluster.topology.isEmpty ? nil : cluster.topology
        }
    }

    /// "last backup 3 hours ago · failed 1 hour ago · schedule 0 3 * * *", nil without any backup.
    private var backups: String? {
        guard cluster.lastBackupAt > 0 || cluster.lastBackupFailedAt > 0 || !cluster.backupSchedule.isEmpty else { return nil }
        return [String(localized: "last backup \(relativeTime(cluster.lastBackupAt))"),
                cluster.lastBackupFailedAt > 0 ? String(localized: "failed \(relativeTime(cluster.lastBackupFailedAt))") : nil,
                cluster.backupSchedule.isEmpty ? nil : String(localized: "schedule \(cluster.backupSchedule)")]
            .compactMap { $0 }.joined(separator: " · ")
    }

    private func reasonText(_ reason: MariaDbReason) -> String {
        switch reason {
        case .noReady: String(localized: "no pod ready")
        case .noPrimary: String(localized: "no primary")
        case .pods: String(localized: "replicas missing")
        case .galeraRecovery: String(localized: "Galera recovery in progress")
        case .backupFailed: String(localized: "last backup failed")
        case .backupStale: String(localized: "no recent backup")
        // The operator's own words (switching primary, updating...).
        case .notReady: cluster.message.isEmpty ? String(localized: "not ready") : String(localized: "operator: \(cluster.message)")
        }
    }

    private func podLine(_ pod: MariaDbPod) -> String {
        let role: String? = switch pod.role {
        case "primary": String(localized: "primary")
        case "replica": String(localized: "replica")
        case "member": String(localized: "member")
        default: pod.role.isEmpty ? nil : pod.role
        }
        let place: String = if pod.node.isEmpty && pod.phase == "Pending" {
            String(localized: "pending, not scheduled")
        } else if !pod.node.isEmpty {
            String(localized: "on \(pod.node)")
        } else {
            pod.phase
        }
        return [pod.name, role, place].compactMap { $0 }.joined(separator: " · ")
    }
}
