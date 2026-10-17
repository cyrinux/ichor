import SwiftUI
import IchorCore

/// A Longhorn volume in two lines: its state, replicas and size, then why a replica cannot be
/// placed when it cannot. What its engine is doing (rebuild, backup, restore) adds a bar; its
/// snapshots and last backup sit at the trailing edge and open the snapshots sheet. The actions
/// are in the row's context menu and swipe actions, none in the row.
struct LonghornVolumeRow: View {
    let volume: LonghornVolume
    let busy: Bool
    /// The default engine's version, for the hint on a volume that lags it.
    var availableEngine = ""
    let openSnapshots: () -> Void

    var body: some View {
        ResourceRow(health: volume.health, title: volume.label, subtitle: facts, trailing: trailing) {
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
                Text(verbatim: volume.scheduleError).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            if volume.tooManySnapshots {
                Text("Close to the snapshot limit").font(.caption).foregroundStyle(attentionColor)
            }
            if volume.engineUpgrading {
                Text("Engine upgrading").font(.caption).foregroundStyle(.secondary)
            } else if volume.engineOutdated && !volume.engineLabel.isEmpty && !availableEngine.isEmpty {
                Text("Engine \(volume.engineLabel), \(availableEngine) available").font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var trailing: some View {
        if busy {
            ProgressView()
        } else {
            LonghornProtectionLine(volume: volume, compact: true, open: openSnapshots)
        }
    }

    /// "detached · faulted · 0/1 replicas · 58 MiB of 1 GiB".
    private var facts: String {
        var parts = [stateLabel(volume.state)]
        if volume.health == .critical || (!volume.robustness.isEmpty && volume.robustness != "unknown") {
            parts.append(robustnessLabel(volume.robustness))
        }
        parts.append(String(localized: "\(volume.replicasHealthy)/\(volume.replicasDesired) replicas"))
        if volume.rebuilding > 0 { parts.append(String(localized: "\(volume.rebuilding) rebuilding")) }
        parts.append(String(localized: "\(formatBytes(volume.actualSize)) of \(formatBytes(volume.size))"))
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

/// A backup target: its URL, its availability as the dot, and why it is unavailable.
struct LonghornBackupTargetRow: View {
    let target: LonghornBackupTarget

    var body: some View {
        ResourceRow(health: target.available ? .ok : .critical, title: String(localized: "Backup target"),
                    subtitle: target.url, mono: false) {
            if !target.available && !target.message.isEmpty {
                Text(verbatim: target.message).font(.caption).foregroundStyle(.statusBad)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// A Longhorn node: ready or not, whether it takes new replicas or is being emptied, its replica
/// count, and one meter per disk. A disk's full path shows only when the nodes do not all share
/// it (`sharedPath`): the list then says it once.
struct LonghornNodeRow: View {
    let node: LonghornNode
    let busy: Bool
    let sharedPath: String?

    var body: some View {
        ResourceRow(health: health, title: node.name, subtitle: state, trailing: trailing) {
            ForEach(Array(node.disks.enumerated()), id: \.offset) { _, disk in
                VStack(alignment: .leading, spacing: 2) {
                    MeterRow(label: longhornDiskLabel(disk.path),
                             fraction: disk.maximum > 0 ? Double(disk.scheduled) / Double(disk.maximum) : 0,
                             value: "\(formatBytes(disk.scheduled)) / \(formatBytes(disk.maximum))")
                    if (sharedPath == nil && !disk.path.isEmpty) || !disk.schedulable {
                        Text(verbatim: [sharedPath == nil ? disk.path.nonEmpty : nil,
                                        disk.schedulable ? nil : String(localized: "scheduling disabled")].compactMap { $0 }.joined(separator: " · "))
                            .font(.caption)
                            .foregroundStyle(disk.schedulable ? Color.secondary : attentionColor)
                    }
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var health: ServiceHealth {
        if !node.ready { return .critical }
        if node.evictionRequested || !node.allowScheduling || !node.schedulable { return .warning }
        return .ok
    }

    /// Why the node is not a plain ready one; nil when it is.
    private var state: String? {
        if !node.ready { return String(localized: "not ready") }
        if node.evictionRequested { return String(localized: "Evicting · \(node.replicas) replicas left") }
        if !node.allowScheduling { return String(localized: "Scheduling off") }
        if !node.schedulable { return String(localized: "scheduling disabled") }
        return nil
    }

    @ViewBuilder private var trailing: some View {
        if busy {
            ProgressView()
        } else if !node.evictionRequested {
            Tag(String(localized: "\(node.replicas) replicas"))
        }
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
