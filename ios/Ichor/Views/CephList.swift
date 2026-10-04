import SwiftUI
import IchorCore

/// Every Ceph cluster (problems first) with its health checks and capacity, its OSDs, then pools and stores.
struct CephList: View {
    let status: CephStatus
    let refresh: () async -> Void

    var body: some View {
        // The namespace only tells OSDs apart when Rook runs more than one cluster.
        let manyClusters = Set(status.osds.map(\.namespace)).count > 1
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                if status.clusters.isEmpty {
                    Text("No Ceph clusters.").foregroundStyle(.secondary)
                }
                ForEach(status.clusters) { ClusterRow(cluster: $0) }
            }
            if !status.osds.isEmpty {
                Section("OSDs") { ForEach(status.osds) { OSDRow(osd: $0, withNamespace: manyClusters) } }
            }
            if !status.pools.isEmpty {
                Section("Pools and stores") { ForEach(status.pools) { PoolRow(pool: $0) } }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

private struct ClusterRow: View {
    let cluster: CephCluster

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 12) {
                HealthDot(health: cluster.health)
                Text(verbatim: cluster.label).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                if !cluster.cephHealth.isEmpty {
                    Text(verbatim: cluster.cephHealth).font(.caption.monospaced()).foregroundStyle(cephHealthColor(cluster.cephHealth))
                }
            }
            Text(verbatim: summary)
                .font(.caption)
                .foregroundStyle(cluster.health.needsAttention ? cluster.health.color : .secondary)
            if cluster.bytesTotal > 0 {
                ProgressView(value: min(1, cluster.usedFraction))
                    .tint(cluster.usedFraction > 0.95 ? .red : cluster.usedFraction > 0.85 ? attentionColor : .accentColor)
                Text("\(formatBytes(cluster.bytesUsed)) of \(formatBytes(cluster.bytesTotal)) used (\(String(format: "%.0f%%", cluster.usedFraction * 100)))")
                    .font(.caption).foregroundStyle(.secondary)
            }
            // Rook's message only says something when Ceph has not (a cluster being set up, a failure).
            if !cluster.message.isEmpty && cluster.checks.isEmpty && cluster.phase != "Ready" && cluster.phase != "Connected" {
                Text(verbatim: cluster.message).font(.caption).foregroundStyle(.secondary)
            }
            ForEach(cluster.checks) { check in
                Text(verbatim: "\(check.name): \(check.message)").font(.caption).foregroundStyle(cephHealthColor(check.severity))
            }
        }
    }

    private var summary: String {
        var parts: [String] = []
        if cluster.external { parts.append(String(localized: "external")) }
        if cluster.osdsTotal > 0 { parts.append(String(localized: "OSDs \(cluster.osdsUp)/\(cluster.osdsTotal) up")) }
        if cluster.monsTotal > 0 { parts.append(String(localized: "mons \(cluster.monsReady)/\(cluster.monsTotal) ready")) }
        if !cluster.version.isEmpty { parts.append(cluster.version) }
        return (parts + cluster.reasons.map(reasonText)).joined(separator: " · ")
    }

    private func reasonText(_ reason: CephReason) -> String {
        switch reason {
        case .healthErr: String(localized: "Ceph reports errors")
        case .failure: String(localized: "Rook reports a failure")
        case .full: String(localized: "full")
        case .noOSD: String(localized: "no OSD up")
        case .noQuorum: String(localized: "mon quorum lost")
        case .healthWarn: String(localized: "Ceph reports warnings")
        case .nearFull: String(localized: "nearly full")
        case .osds: String(localized: "OSDs down")
        case .mons: String(localized: "mons down")
        // Rook's phase is its own word (Progressing, Updating...).
        case .notReady: String(localized: "operator: \(cluster.phase)")
        }
    }
}

private struct OSDRow: View {
    let osd: CephOSD
    let withNamespace: Bool

    var body: some View {
        HStack(spacing: 6) {
            HealthDot(health: osd.ready ? .ok : .critical, size: 8)
            Text(verbatim: line).font(.caption.monospaced()).foregroundStyle(osd.ready ? .primary : Color.red)
        }
    }

    private var line: String {
        let place: String = if osd.node.isEmpty && osd.phase == "Pending" {
            String(localized: "pending, not scheduled")
        } else if !osd.node.isEmpty {
            String(localized: "on \(osd.node)")
        } else {
            osd.phase
        }
        return [withNamespace ? osd.namespace : nil, "osd.\(osd.osdID)", place].compactMap { $0 }.joined(separator: " · ")
    }
}

private struct PoolRow: View {
    let pool: CephPool

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            HealthDot(health: pool.health).padding(.top, 5)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: pool.label).font(.subheadline.monospaced()).lineLimit(1)
                Text(verbatim: [kindLabel, pool.phase.isEmpty ? nil : pool.phase].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(pool.health.needsAttention ? pool.health.color : .secondary)
            }
        }
    }

    private var kindLabel: String {
        switch pool.kind {
        case "blockPool": String(localized: "block pool")
        case "filesystem": String(localized: "filesystem")
        case "objectStore": String(localized: "object store")
        default: pool.kind
        }
    }
}

private func cephHealthColor(_ health: String) -> Color {
    switch health {
    case "HEALTH_ERR": .red
    case "HEALTH_WARN": attentionColor
    case "HEALTH_OK": .green
    default: .secondary
    }
}
