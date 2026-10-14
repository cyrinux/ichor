import SwiftUI
import IchorCore

/// Velero's storage locations, its schedules (problems first) with their last backup, opening to
/// the details (back up now, the schedule's backups), and the backups taken by hand that failed
/// in the last week.
struct VeleroList: View {
    let status: VeleroStatus
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var confirmBackup: VeleroSchedule?
    @State private var deleteTarget: VeleroBackupTarget?
    @State private var restoreTarget: VeleroRestoreTarget?
    /// Schedule labels with a backup now in flight, and "namespace/backup" with a deletion in flight.
    @State private var running: Set<String> = []
    /// Bumped after each action: the open backup lists reload.
    @State private var changes = 0
    @State private var resultMessage: String?

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
                ForEach(status.schedules) { schedule in
                    ScheduleRow(schedule: schedule, busy: running.contains(schedule.label), changes: changes, deleting: running,
                                onBackupNow: { confirmBackup = schedule }, onDelete: { deleteTarget = $0 }, onRestore: { restoreTarget = $0 })
                }
            }
            if !status.adhoc.isEmpty {
                Section("Failed manual backups (last 7 days)") {
                    ForEach(status.adhoc) { adhoc in
                        AdhocRow(backup: adhoc, deleting: running.contains(adhoc.label)) {
                            deleteTarget = VeleroBackupTarget(namespace: adhoc.namespace, name: adhoc.backup.name)
                        } onRestore: {
                            restoreTarget = VeleroRestoreTarget(namespace: adhoc.namespace, name: adhoc.backup.name)
                        }
                    }
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
        .loadsKubeActionAccess(namespace: status.schedules.first?.namespace ?? status.adhoc.first?.namespace)
        .confirmationDialog(confirmBackup.map { String(localized: "Back up \($0.label) now?") } ?? "",
                            isPresented: $confirmBackup.isPresent(),
                            titleVisibility: .visible,
                            presenting: confirmBackup) { schedule in
            Button("Back up now") { Task { await backupNow(schedule) } }
            Button("Cancel", role: .cancel) {}
        } message: { schedule in
            Text(backupNowMessage(schedule))
        }
        .sheet(item: $deleteTarget) { target in
            VeleroDeleteBackupSheet(target: target) {
                deleteTarget = nil
                Task { await delete(target) }
            }.loadsKubeActionAccess(namespace: target.namespace)
        }
        .sheet(item: $restoreTarget) { target in
            VeleroRestoreSheet(target: target, refresh: refresh)
                .loadsKubeActionAccess(namespace: target.namespace)
        }
        .messageAlert($resultMessage)
    }

    private func backupNowMessage(_ schedule: VeleroSchedule) -> String {
        let namespaces = schedule.includedNamespaces.filter { $0 != "*" }
        let list = namespaces.isEmpty ? String(localized: "all") : namespaces.joined(separator: ", ")
        let location = schedule.storageLocation.isEmpty ? "-" : schedule.storageLocation
        return String(localized: "Velero starts a backup with this schedule's settings (namespaces: \(list), storage location: \(location)). It runs next to the scheduled ones and expires like them.")
    }

    private func backupNow(_ schedule: VeleroSchedule) async {
        guard let client = model.client, !running.contains(schedule.label) else { return }
        running.insert(schedule.label)
        defer { running.remove(schedule.label) }
        do {
            let name = try await client.veleroBackupNow(namespace: schedule.namespace, schedule: schedule.name)
            resultMessage = String(localized: "Backup \(name) started")
            changes += 1
            await refresh()
        } catch { resultMessage = String(localized: "Could not back up \(schedule.label): \(error.localizedDescription)") }
    }

    private func delete(_ target: VeleroBackupTarget) async {
        guard let client = model.client, !running.contains(target.id) else { return }
        running.insert(target.id)
        defer { running.remove(target.id) }
        do {
            _ = try await client.veleroDeleteBackup(namespace: target.namespace, backup: target.name)
            resultMessage = String(localized: "Deletion of \(target.name) requested")
            changes += 1
            await refresh()
        } catch { resultMessage = String(localized: "Could not delete \(target.name): \(error.localizedDescription)") }
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
    let busy: Bool
    let changes: Int
    let deleting: Set<String>
    let onBackupNow: () -> Void
    let onDelete: (VeleroBackupTarget) -> Void
    let onRestore: (VeleroRestoreTarget) -> Void

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
        Button(action: onBackupNow) {
            if busy { ProgressView() } else { Text("Back up now") }
        }
        .disabled(busy || schedule.backupNowBlocker != nil)
        .kubeGated(.veleroBackupNow, in: schedule.namespace)
        if let blocker = schedule.backupNowBlocker {
            Text(blockerText(blocker)).font(.caption).foregroundStyle(.secondary)
        }
        KubeDeniedNote(.veleroBackupNow, in: schedule.namespace)
        VeleroBackupsDisclosure(schedule: schedule, reload: changes, deleting: deleting, onDelete: onDelete, onRestore: onRestore)
    }

    private func blockerText(_ blocker: VeleroBackupBlocker) -> String {
        switch blocker {
        case .paused: String(localized: "The schedule is paused: unpause it to back it up")
        case .invalid: String(localized: "The schedule failed validation")
        case .running: String(localized: "A backup of this schedule is running")
        }
    }
}

private struct AdhocRow: View {
    let backup: VeleroAdhocBackup
    let deleting: Bool
    let onDelete: () -> Void
    var onRestore: (() -> Void)?

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            HealthDot(health: backup.health).padding(.top, 5)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: backup.label).font(.subheadline.monospaced()).lineLimit(1)
                Text(verbatim: line).font(.caption).foregroundStyle(backup.health.color).lineLimit(2)
            }
            Spacer()
            if deleting {
                Text("deleting…").font(.caption).foregroundStyle(.secondary)
            } else {
                // Only a partially failed backup can be a restore source; the others failed for good.
                if backup.backup.phase == "PartiallyFailed", let onRestore {
                    Button("Restore", action: onRestore).buttonStyle(.borderless).font(.caption)
                        .kubeGated(.veleroRestore, in: backup.namespace)
                }
                Button("Delete", role: .destructive, action: onDelete).buttonStyle(.borderless).font(.caption)
                    .kubeGated(.veleroDeleteBackup, in: backup.namespace)
            }
        }
    }

    private var line: String {
        let b = backup.backup
        let at = b.completedAt > 0 ? b.completedAt : b.startedAt
        return [b.phase, at > 0 ? relativeTime(at) : nil, countsText(errors: b.errors, warnings: b.warnings),
                b.failureReason.nonEmpty].compactMap { $0 }.joined(separator: " · ")
    }
}
