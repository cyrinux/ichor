import SwiftUI
import IchorCore

/// Velero's storage locations, its schedules (problems first) with their last backup, opening to
/// the details, and the backups taken by hand that failed in the last week.
struct VeleroList: View {
    let status: VeleroStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            if !status.locations.isEmpty {
                Section { ForEach(status.locations) { LocationRow(location: $0) } }
            }
            Section {
                if status.schedules.isEmpty {
                    Text("No Velero schedules.").foregroundStyle(.secondary)
                }
                ForEach(status.schedules) { ScheduleRow(schedule: $0) }
            }
            if !status.adhoc.isEmpty {
                Section("Failed manual backups (last 7 days)") { ForEach(status.adhoc) { AdhocRow(backup: $0) } }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

/// "2 errors, 3 warnings", nil when both are 0.
private func countsText(errors: Int, warnings: Int) -> String? {
    let parts = [errors > 0 ? String(localized: "\(errors) errors") : nil,
                 warnings > 0 ? String(localized: "\(warnings) warnings") : nil].compactMap { $0 }
    return parts.isEmpty ? nil : parts.joined(separator: ", ")
}

private struct LocationRow: View {
    let location: VeleroLocation

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                VStack(alignment: .leading, spacing: 1) {
                    Text(verbatim: [String(localized: "Storage location \(location.name)"), location.isDefault ? String(localized: "default") : nil]
                        .compactMap { $0 }.joined(separator: " · "))
                        .font(.subheadline)
                    Text(verbatim: [location.provider, location.bucket].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                switch location.phase {
                case "Available": StatusPill(label: String(localized: "available"), color: .green)
                case "Unavailable": StatusPill(label: String(localized: "unavailable"), color: .red)
                default: EmptyView()
                }
            }
            if location.phase == "Unavailable" && !location.message.isEmpty {
                Text(verbatim: location.message).font(.caption).foregroundStyle(.red).lineLimit(3)
            }
        }
    }
}

private struct ScheduleRow: View {
    let schedule: VeleroSchedule

    var body: some View {
        DisclosureGroup {
            details
        } label: {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: schedule.health).padding(.top, 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: schedule.label).font(.subheadline.monospaced()).lineLimit(1)
                    Text(verbatim: summary)
                        .font(.caption)
                        .foregroundStyle(schedule.health.needsAttention ? schedule.health.color : .secondary)
                        .lineLimit(2)
                }
            }
        }
    }

    /// The last backup ("Completed 3 hours ago"), then paused, running and the reasons.
    private var summary: String {
        var parts: [String]
        if let b = schedule.lastBackup {
            let at = b.completedAt > 0 ? b.completedAt : b.startedAt
            parts = [at > 0 ? "\(b.phase) \(relativeTime(at))" : b.phase]
        } else {
            parts = [String(localized: "no backup yet")]
        }
        if schedule.paused { parts.append(String(localized: "paused")) }
        if schedule.inProgress { parts.append(String(localized: "backup running")) }
        parts += schedule.reasons.map(reasonText)
        return parts.joined(separator: " · ")
    }

    private func reasonText(_ reason: VeleroReason) -> String {
        switch reason {
        case .failed: String(localized: "last backup failed")
        case .location: String(localized: "storage location unavailable")
        case .partiallyFailed: String(localized: "last backup partially failed")
        case .stale: String(localized: "no recent backup")
        case .invalid: String(localized: "invalid schedule")
        }
    }

    @ViewBuilder private var details: some View {
        LabeledContent("Schedule", value: schedule.schedule)
        if !schedule.storageLocation.isEmpty { LabeledContent("Storage location", value: schedule.storageLocation) }
        let namespaces = schedule.includedNamespaces.filter { $0 != "*" }
        LabeledContent("Namespaces", value: namespaces.isEmpty ? String(localized: "all") : namespaces.joined(separator: ", "))
        if let b = schedule.lastBackup {
            LabeledContent("Last backup", value: b.name)
            LabeledContent("Phase", value: [b.phase, countsText(errors: b.errors, warnings: b.warnings)].compactMap { $0 }.joined(separator: " · "))
        }
        LabeledContent("Last success", value: relativeTime(schedule.lastSuccessAt))
        if let reason = schedule.lastBackup?.failureReason, !reason.isEmpty {
            Text(verbatim: reason).font(.caption).foregroundStyle(.red)
        }
        ForEach(schedule.validationErrors, id: \.self) { Text(verbatim: $0).font(.caption).foregroundStyle(.orange) }
    }
}

private struct AdhocRow: View {
    let backup: VeleroAdhocBackup

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            HealthDot(health: backup.health).padding(.top, 5)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: backup.label).font(.subheadline.monospaced()).lineLimit(1)
                Text(verbatim: line).font(.caption).foregroundStyle(backup.health.color).lineLimit(2)
            }
        }
    }

    private var line: String {
        let b = backup.backup
        let at = b.completedAt > 0 ? b.completedAt : b.startedAt
        return [b.phase, at > 0 ? relativeTime(at) : nil, countsText(errors: b.errors, warnings: b.warnings),
                b.failureReason.isEmpty ? nil : b.failureReason].compactMap { $0 }.joined(separator: " · ")
    }
}
