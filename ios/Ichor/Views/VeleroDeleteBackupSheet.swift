import SwiftUI
import IchorCore

/// A Velero backup to delete: its namespace and name.
struct VeleroBackupTarget: Identifiable, Hashable {
    let namespace: String
    let name: String
    var id: String { "\(namespace)/\(name)" }
}

/// Deleting a backup removes its data from the object store: the backup's name is typed to confirm.
struct VeleroDeleteBackupSheet: View {
    let target: VeleroBackupTarget
    let onConfirm: () -> Void
    @Environment(\.kubeActionAccess) private var access

    private var denial: KubeAccess? { access?.denial(for: .veleroDeleteBackup, in: target.namespace) }
    private var message: String {
        var lines = [String(localized: "Velero removes this backup's data from its storage location, then the backup itself. It cannot be restored afterwards.")]
        if let denial { lines.append(denial.deniedText) }
        return lines.joined(separator: "\n\n")
    }

    var body: some View {
        HostnameConfirmationSheet(title: String(localized: "Delete backup \(target.name)?"), message: message, hostname: target.name,
                                  actionTitle: String(localized: "Delete backup"), enabled: denial == nil, onConfirm: onConfirm)
    }
}

/// A schedule's backups, loaded when opened and again after each action (`reload`), with Delete
/// on the finished ones.
struct VeleroBackupsDisclosure: View {
    let schedule: VeleroSchedule
    let reload: Int
    let deleting: Set<String>
    let onDelete: (VeleroBackupTarget) -> Void

    @Environment(AppModel.self) private var model
    @State private var open = false
    @State private var list: VeleroBackups?
    @State private var error: String?

    var body: some View {
        DisclosureGroup("Backups", isExpanded: $open) {
            if let error { ErrorLine(error: error) }
            if let list {
                if !list.error.isEmpty { ErrorLine(error: list.error) }
                if list.backups.isEmpty { Text("No backups left").font(.caption).foregroundStyle(.secondary) }
                ForEach(list.backups) { backup in
                    BackupRow(backup: backup, deleting: backup.deleting || deleting.contains("\(schedule.namespace)/\(backup.name)")) {
                        onDelete(VeleroBackupTarget(namespace: schedule.namespace, name: backup.name))
                    }
                }
            } else if error == nil {
                ProgressView()
            }
        }
        .task(id: open ? reload : -1) {
            guard open, let client = model.client else { return }
            do {
                list = try await client.veleroBackups(namespace: schedule.namespace, schedule: schedule.name)
                error = nil
            } catch {
                if !Task.isCancelled { self.error = error.localizedDescription }
            }
        }
    }
}

private struct BackupRow: View {
    let backup: VeleroBackupDetail
    let deleting: Bool
    let onDelete: () -> Void

    var body: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: backup.name).font(.caption.monospaced()).lineLimit(1)
                Text(verbatim: line).font(.caption2)
                    .foregroundStyle(backup.phase == "Failed" || backup.phase == "PartiallyFailed" ? .orange : .secondary)
                if !backup.failureReason.isEmpty { Text(verbatim: backup.failureReason).font(.caption2).foregroundStyle(.red) }
                if !backup.deleteErrors.isEmpty {
                    Text("Deletion failed: \(backup.deleteErrors.joined(separator: "; "))").font(.caption2).foregroundStyle(.red)
                }
            }
            Spacer()
            if backup.deletable && !deleting {
                Button("Delete", role: .destructive, action: onDelete).buttonStyle(.borderless).font(.caption)
            }
        }
    }

    private var line: String {
        var parts = [backup.phase.nonEmpty, backup.at > 0 ? relativeTime(backup.at) : nil]
        if backup.errors > 0 { parts.append(String(localized: "\(backup.errors) errors")) }
        if backup.warnings > 0 { parts.append(String(localized: "\(backup.warnings) warnings")) }
        if backup.expiresAt > 0 {
            let date = Date(timeIntervalSince1970: Double(backup.expiresAt) / 1000).formatted(date: .abbreviated, time: .omitted)
            parts.append(String(localized: "expires \(date)"))
        }
        if deleting { parts.append(String(localized: "deleting…")) }
        return parts.compactMap { $0 }.joined(separator: " · ")
    }
}
