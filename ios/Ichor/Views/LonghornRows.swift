import SwiftUI
import IchorCore

/// A Longhorn volume: its state and replicas, what its engine is doing (rebuild, backup,
/// restore) and why a replica cannot be placed.
struct LonghornVolumeRow: View {
    let volume: LonghornVolume
    let busy: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            HealthDot(health: volume.health).padding(.top, 5)
            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(verbatim: volume.label).font(.subheadline.monospaced()).lineLimit(1)
                    Spacer()
                    if busy { ProgressView() }
                }
                Text(verbatim: facts).font(.caption).foregroundStyle(volume.health.needsAttention ? volume.health.color : .secondary)
                Text(verbatim: details).font(.caption).foregroundStyle(.secondary).lineLimit(2).monospacedDigit()
                // 0 is also what an engine that could not be read gives: the facts line says it rebuilds.
                if volume.rebuilding > 0 && volume.rebuildProgress > 0 {
                    ProgressLine(label: String(localized: "Rebuilding \(volume.rebuildProgress)%"), percent: volume.rebuildProgress)
                }
                if volume.backingUp {
                    ProgressLine(label: String(localized: "Backing up \(volume.backupProgress)%"), percent: volume.backupProgress)
                }
                if volume.restoring {
                    ProgressLine(label: String(localized: "Restoring \(volume.restoreProgress)%"), percent: volume.restoreProgress)
                }
                if !volume.scheduleError.isEmpty {
                    Text(verbatim: volume.scheduleError).font(.caption).foregroundStyle(attentionColor)
                }
                if volume.tooManySnapshots {
                    Text("Close to the snapshot limit").font(.caption).foregroundStyle(attentionColor)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var facts: String {
        var parts = [stateLabel(volume.state)]
        if volume.health == .critical || (!volume.robustness.isEmpty && volume.robustness != "unknown") {
            parts.append(robustnessLabel(volume.robustness))
        }
        parts.append(String(localized: "\(volume.replicasHealthy)/\(volume.replicasDesired) replicas"))
        if volume.rebuilding > 0 { parts.append(String(localized: "\(volume.rebuilding) rebuilding")) }
        return parts.joined(separator: " · ")
    }

    private var details: String {
        var parts: [String] = []
        if !volume.node.isEmpty {
            parts.append(String(localized: "on \(volume.node)"))
        } else if !volume.replicaNodes.isEmpty {
            parts.append(volume.replicaNodes.joined(separator: ", "))
        }
        parts.append("\(formatBytes(volume.actualSize)) / \(formatBytes(volume.size))")
        parts.append(volume.lastBackupAt > 0 ? String(localized: "backup \(relativeTime(volume.lastBackupAt))") : String(localized: "never backed up"))
        return parts.joined(separator: " · ")
    }
}

/// An operation in flight: its label with the percentage, then a thin bar.
private struct ProgressLine: View {
    let label: String
    let percent: Int

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: label).font(.caption).foregroundStyle(.secondary).monospacedDigit()
            ProgressView(value: Double(min(100, max(0, percent))), total: 100)
        }
        .padding(.top, 2)
    }
}

struct LonghornBackupTargetRow: View {
    let target: LonghornBackupTarget

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                VStack(alignment: .leading, spacing: 1) {
                    Text("Backup target").font(.subheadline)
                    Text(verbatim: target.url).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                StatusPill(label: target.available ? String(localized: "available") : String(localized: "unavailable"),
                           color: target.available ? .green : .red)
            }
            if !target.available && !target.message.isEmpty {
                Text(verbatim: target.message).font(.caption).foregroundStyle(.statusBad)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// A Longhorn node: ready or not, whether it takes new replicas or is being emptied, and its disks.
struct LonghornNodeRow: View {
    let node: LonghornNode
    let busy: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                HealthDot(health: node.ready ? .ok : .critical)
                Text(verbatim: node.name).font(.subheadline.monospaced())
                Spacer()
                if busy {
                    ProgressView()
                } else if !node.ready {
                    Text("not ready").font(.caption).foregroundStyle(.statusBad)
                } else if node.evictionRequested {
                    Text("Evicting · \(node.replicas) replicas left").font(.caption).foregroundStyle(attentionColor)
                } else if !node.allowScheduling {
                    Text("Scheduling off").font(.caption).foregroundStyle(attentionColor)
                } else if !node.schedulable {
                    Text("scheduling disabled").font(.caption).foregroundStyle(attentionColor)
                }
            }
            ForEach(Array(node.disks.enumerated()), id: \.offset) { _, disk in
                if disk.maximum > 0 {
                    ProgressView(value: min(1, Double(disk.scheduled) / Double(disk.maximum))).tint(disk.schedulable ? .accentColor : attentionColor)
                }
                Text(verbatim: [disk.path.isEmpty ? nil : disk.path,
                                String(localized: "\(formatBytes(disk.scheduled)) of \(formatBytes(disk.maximum)) scheduled"),
                                disk.schedulable ? nil : String(localized: "scheduling disabled")].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private func stateLabel(_ state: String) -> String {
    switch state {
    case "attached": String(localized: "attached")
    case "detached": String(localized: "detached")
    case "attaching": String(localized: "attaching")
    case "detaching": String(localized: "detaching")
    case "creating": String(localized: "creating")
    case "deleting": String(localized: "deleting")
    default: state
    }
}

private func robustnessLabel(_ robustness: String) -> String {
    switch robustness {
    case "healthy": String(localized: "healthy")
    case "degraded": String(localized: "degraded")
    case "faulted": String(localized: "faulted")
    default: String(localized: "unknown")
    }
}
