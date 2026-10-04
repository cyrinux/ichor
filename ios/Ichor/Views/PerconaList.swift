import SwiftUI
import IchorCore

/// Every Percona XtraDB cluster (problems first) with its members, its proxy and its backups.
struct PerconaList: View {
    let status: PerconaStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                if status.clusters.isEmpty {
                    Text("No Percona XtraDB clusters.").foregroundStyle(.secondary)
                }
                ForEach(status.clusters) { ClusterRow(cluster: $0) }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

private struct ClusterRow: View {
    let cluster: PerconaCluster

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 12) {
                HealthDot(health: cluster.health)
                Text(verbatim: cluster.label).font(.subheadline.monospaced()).lineLimit(1)
            }
            Text(verbatim: summary)
                .font(.caption)
                .foregroundStyle(cluster.health.needsAttention ? cluster.health.color : .secondary)
            if !cluster.message.isEmpty && cluster.health.needsAttention {
                Text(verbatim: cluster.message).font(.caption).foregroundStyle(.secondary)
            }
            if let backups {
                Text(verbatim: backups).font(.caption).foregroundStyle(.secondary)
            }
            ForEach(cluster.pods) { pod in
                HStack(spacing: 6) {
                    HealthDot(health: pod.ready ? .ok : .critical, size: 8)
                    Text(verbatim: podLine(pod)).font(.caption.monospaced()).foregroundStyle(pod.ready ? .primary : Color.red)
                }
            }
        }
    }

    private var summary: String {
        let members = cluster.paused ? String(localized: "paused") : String(localized: "\(cluster.pxcReady)/\(cluster.pxcSize) ready")
        // Product names stay untranslated: "HAProxy 2/2".
        let proxy: String? = switch cluster.proxy {
        case "haproxy": "HAProxy \(cluster.proxyReady)/\(cluster.proxySize)"
        case "proxysql": "ProxySQL \(cluster.proxyReady)/\(cluster.proxySize)"
        default: nil
        }
        return ([members, proxy].compactMap { $0 } + cluster.reasons.map(reasonText)).joined(separator: " · ")
    }

    /// Last success and failure, and the schedules that should keep them coming; nil without either.
    private var backups: String? {
        if cluster.backupSchedules.isEmpty && cluster.lastBackupAt == 0 && cluster.lastBackupFailedAt == 0 { return nil }
        var parts = [String(localized: "last backup \(relativeTime(cluster.lastBackupAt))")]
        if cluster.lastBackupFailedAt > 0 { parts.append(String(localized: "last failure \(relativeTime(cluster.lastBackupFailedAt))")) }
        if !cluster.backupSchedules.isEmpty {
            let schedules = cluster.backupSchedules.map { "\($0.name) (\($0.schedule))" }.joined(separator: ", ")
            parts.append(String(localized: "schedules: \(schedules)"))
        }
        return parts.joined(separator: " · ")
    }

    private func reasonText(_ reason: PerconaReason) -> String {
        switch reason {
        case .error: String(localized: "operator error")
        case .noMember: String(localized: "no member ready")
        case .members: String(localized: "members missing")
        case .proxy: String(localized: "proxy pods missing")
        case .initializing: String(localized: "initializing")
        case .backupFailed: String(localized: "last backup failed")
        case .backupStale: String(localized: "no recent backup")
        }
    }

    private func podLine(_ pod: PerconaPod) -> String {
        let place: String = if pod.node.isEmpty && pod.phase == "Pending" {
            String(localized: "pending, not scheduled")
        } else if !pod.node.isEmpty {
            String(localized: "on \(pod.node)")
        } else {
            pod.phase
        }
        return [pod.name, place].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}
