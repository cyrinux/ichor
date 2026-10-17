import SwiftUI
import IchorCore

/// The cluster at a glance, above the nodes: its name and overall state, how many nodes on
/// which Talos version, and the capacity of the nodes that answered. With `live` usage, CPU
/// and memory follow it; without, they show the overview's snapshot. Same as Android's
/// ClusterSummaryCard; the cluster insights row under it is its footer.
struct ClusterSummaryCard: View {
    let name: String
    let summary: ClusterSummary
    /// While the overview streams in: how many nodes answered, shown instead of the health
    /// pill (which only a complete overview can tell), with a thin bar when the total is known.
    var answered: NodesAnswered? = nil
    let live: ClusterLive?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: name).font(.headline).lineLimit(1)
                    Text(verbatim: headline).font(.caption).foregroundStyle(.secondary).monospacedDigit()
                }
                Spacer()
                if let answered {
                    AnsweredLabel(answered: answered)
                } else {
                    ClusterStatusPill(status: summary.status)
                }
            }
            if let fraction = answered?.fraction {
                ProgressView(value: min(max(fraction, 0), 1))
                    .animation(.easeInOut, value: fraction)
                    .accessibilityHidden(true) // the label above says the same
            }
            Breakdown(summary: summary)
            Divider()
            HStack(alignment: .top, spacing: 12) {
                StatTile(icon: "cube", value: "\(summary.ready)/\(summary.total)", label: String(localized: "Nodes"))
                    .frame(maxWidth: .infinity, alignment: .leading)
                CpuStat(cpuCount: summary.cpuCount, live: live)
                    .frame(maxWidth: .infinity, alignment: .leading)
                // Live memory only once it covers every node the overview reached, never fewer.
                MemoryStat(summary: summary, live: live?.usage.flatMap { $0.nodes >= summary.total - summary.unreachable ? $0 : nil })
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.vertical, 4)
    }

    /// "3 nodes  ·  v1.11.2", or "v1.10.4 – v1.11.2" while the nodes run different versions.
    private var headline: String {
        [String(localized: "\(summary.total) nodes"), summary.versionSpan].compactMap { $0 }.joined(separator: "  ·  ")
    }
}

/// Unknown capacity (older core, or no node said): a dash rather than a misleading 0.
private let unknownValue = "—"

private struct ClusterStatusPill: View {
    let status: ClusterStatus

    var body: some View {
        switch status {
        case .healthy: StatusPill(label: String(localized: "Healthy"), color: .statusOK)
        case .degraded: StatusPill(label: String(localized: "Degraded"), color: .statusWarn)
        case .down: StatusPill(label: String(localized: "Unreachable"), color: .statusBad)
        }
    }
}

/// "7 of 8 nodes answered", or "Nodes answered: 7" while the total is unknown.
private struct AnsweredLabel: View {
    let answered: NodesAnswered

    var body: some View {
        Group {
            if let total = answered.total {
                Text("\(answered.answered) of \(total) nodes answered")
            } else {
                Text("Nodes answered: \(answered.answered)")
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .monospacedDigit()
        .lineLimit(1)
    }
}

/// Which nodes are in trouble, each count in its own status colour; nothing when all are ready.
private struct Breakdown: View {
    let summary: ClusterSummary

    var body: some View {
        let candidates: [Text?] = [
            summary.notReady > 0 ? Text("\(summary.notReady) not ready").foregroundStyle(.statusWarn) : nil,
            summary.unreachable > 0 ? Text("\(summary.unreachable) unreachable").foregroundStyle(.statusBad) : nil,
        ]
        let parts = candidates.compactMap { $0 }
        if !parts.isEmpty {
            parts.indices.reduce(Text(verbatim: "")) { text, i in
                text + Text(verbatim: i > 0 ? "  ·  " : "").foregroundStyle(.secondary) + parts[i]
            }
            .font(.caption)
        }
    }
}

/// Cores; with live usage, the busy share of them and how it moved over the last minutes.
private struct CpuStat: View {
    let cpuCount: Int
    let live: ClusterLive?

    var body: some View {
        if let used = live?.usage?.cpuFraction, let live {
            VStack(alignment: .leading, spacing: 4) {
                StatTile(icon: "cpu", value: percent(used),
                         label: cpuCount > 0 ? String(localized: "of \(cpuCount) cores") : String(localized: "CPU in use"))
                CpuSparkline(history: live.cpuHistory)
            }
        } else {
            StatTile(icon: "cpu", value: cpuCount > 0 ? "\(cpuCount)" : unknownValue, label: String(localized: "CPU cores"))
        }
    }
}

private struct MemoryStat: View {
    let summary: ClusterSummary
    let live: ClusterUsage?

    var body: some View {
        let total = live.map(\.memTotal).flatMap { $0 > 0 ? $0 : nil } ?? summary.memTotal
        let used = live?.memUsedFraction ?? summary.memUsedFraction
        VStack(alignment: .leading, spacing: 6) {
            StatTile(icon: "memorychip", value: total > 0 ? formatBytes(total) : unknownValue,
                     label: used.map { String(localized: "\(percent($0)) used") } ?? String(localized: "Memory"))
            if let used {
                // Glide between samples rather than jump.
                UsageBar(fraction: used).animation(.easeInOut, value: used)
            }
        }
    }
}

/// "42 %" as the locale writes it.
private func percent(_ fraction: Double) -> String {
    fraction.formatted(.percent.precision(.fractionLength(0)))
}
