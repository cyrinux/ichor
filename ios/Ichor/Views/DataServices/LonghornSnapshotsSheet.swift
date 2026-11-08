import SwiftUI
import IchorCore

/// How well a volume is protected, on its row: its snapshot count and last backup, tinted when
/// the backup is missing, old or failed. Opens the snapshots sheet.
struct LonghornProtectionLine: View {
    let volume: LonghornVolume
    /// Icons only, for a row's trailing edge: the time of the last backup is read out, not shown.
    var compact = false
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            HStack(spacing: 4) {
                Image(systemName: "camera").foregroundStyle(Color.accentColor)
                Text(verbatim: String(volume.snapshots.count)).foregroundStyle(Color.accentColor)
                Image(systemName: volume.latestBackupFailed ? "exclamationmark.icloud" : "checkmark.icloud")
                    .foregroundStyle(tint)
                    .padding(.leading, 4)
                if !compact { Text(verbatim: lastBackup).foregroundStyle(tint) }
                Image(systemName: "chevron.right").foregroundStyle(.tertiary)
            }
            .font(.caption)
            .monospacedDigit()
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(Text("Snapshots & backups"))
        .accessibilityValue(Text(verbatim: lastBackup))
    }

    private var lastBackup: String {
        volume.lastBackupAt > 0
            ? String(localized: "backup \(relativeTime(volume.lastBackupAt))") : String(localized: "never backed up")
    }

    private var tint: Color { volume.protectionColor ?? .accentColor }
}

extension LonghornVolume {
    /// Bad for a failed last backup, warn for a missing or stale one, nil when all is well.
    var protectionColor: Color? {
        if latestBackupFailed { return Color.statusBad }
        if lastBackupAt == 0 || backupStale { return Color.statusWarn }
        return nil
    }
}

/// The snapshots and backups of a volume, newest first, with what can be done to them: take a
/// snapshot, back the volume or one snapshot up, delete a snapshot.
struct LonghornSnapshotsSheet: View {
    let volume: LonghornVolume
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.kubeActionAccess) private var access
    /// Snapshot names with an action in flight; "" is a snapshot being taken, volumeBackupKey the volume backup.
    /// The kube access (\.kubeActionAccess) comes from the presenter's loadsKubeActionAccess.
    @State private var running: Set<String> = []
    @State private var confirmDelete: LonghornSnapshot?
    @State private var resultMessage: String?

    private static let volumeBackupKey = "\u{0}backup"

    var body: some View {
        NavigationStack {
            List {
                Section {
                    summary
                    actionButtons
                    if !volume.attached {
                        Text("Attach the volume to take a snapshot or back it up.").font(.footnote).foregroundStyle(
                            .secondary)
                    }
                    KubeDeniedNote(.longhornSnapshot, in: volume.namespace)
                    if volume.backingUp {
                        ProgressView(value: Double(min(100, max(0, volume.backupProgress))), total: 100)
                    }
                }
                .listRowSeparator(.hidden)
                Section("Snapshots") {
                    if volume.snapshots.isEmpty {
                        Text("No snapshots yet.").foregroundStyle(.secondary)
                    }
                    ForEach(volume.snapshots) { snapshotRow($0) }
                }
                Section {
                    if volume.backups.isEmpty {
                        Text("No backups of this volume.").foregroundStyle(.secondary)
                    }
                    ForEach(volume.backups) { LonghornBackupRow(backup: $0) }
                } header: {
                    Text("Backups")
                } footer: {
                    if volume.backupCount > volume.backups.count {
                        Text("The \(String(volume.backups.count)) newest of \(String(volume.backupCount)) backups")
                    }
                }
            }
            .navigationTitle("Snapshots & backups")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    VStack(spacing: 0) {
                        Text("Snapshots & backups").font(.headline)
                        Text(verbatim: volume.label).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(
                            1)
                    }
                }
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .refreshable { await refresh() }
            .confirmationDialog(
                String(localized: "Delete this snapshot?"),
                isPresented: $confirmDelete.isPresent(),
                titleVisibility: .visible,
                presenting: confirmDelete
            ) { snapshot in
                Button("Delete", role: .destructive) {
                    Task { await run(.delete, snapshot: snapshot.name) }
                }
                Button("Cancel", role: .cancel) {}
            } message: { snapshot in
                let text = String(
                    localized:
                        "Snapshot \(snapshot.name) of \(volume.label) is deleted and its space given back. Backups taken from it stay."
                )
                // Whether a VolumeSnapshot keeps it could not be read.
                if !volume.volumeSnapshotsKnown && snapshot.userCreated {
                    Text(
                        verbatim: text + "\n\n"
                            + String(
                                localized:
                                    "Your account cannot read the VolumeSnapshots: if one was taken from this snapshot, it could no longer be restored. Check with kubectl get volumesnapshots -A first."
                            ))
                } else {
                    Text(verbatim: text)
                }
            }
            .messageAlert($resultMessage)
        }
    }

    private var canAct: Bool { volume.attached && access?.denial(for: .longhornSnapshot, in: volume.namespace) == nil }

    private var summary: some View {
        HStack(spacing: 8) {
            LonghornStatTile(
                label: String(localized: "Snapshots"), value: String(volume.snapshots.count), caption: snapshotsCaption)
            LonghornStatTile(
                label: String(localized: "Last backup"),
                value: volume.lastBackupAt > 0 ? relativeTime(volume.lastBackupAt) : String(localized: "Never"),
                caption: backupCaption,
                accent: volume.protectionColor
            )
            .layoutPriority(1)
            LonghornStatTile(
                label: String(localized: "Backups"), value: String(volume.backupCount), caption: nil,
                accent: volume.backupCount == 0 ? Color.statusWarn : nil)
        }
        .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
    }

    private var snapshotsCaption: String? {
        if volume.snapshotsRemoving > 0 {
            return String(localized: "\(String(volume.snapshotsRemoving)) being removed")
        }
        if let newest = volume.snapshots.first, newest.createdAt > 0 {
            return String(localized: "newest \(relativeTime(newest.createdAt))")
        }
        return nil
    }

    private var backupCaption: String? {
        if volume.latestBackupFailed { return String(localized: "the last one failed") }
        if volume.backupStale { return String(localized: "over a week old") }
        return nil
    }

    private var actionButtons: some View {
        HStack(spacing: 8) {
            Button {
                Task { await run(.take, snapshot: "") }
            } label: {
                ButtonLabel(
                    title: String(localized: "Take snapshot"), systemImage: "camera", busy: running.contains(""))
            }
            .buttonStyle(.borderedProminent)
            .disabled(!canAct || running.contains(""))
            Button {
                Task { await backUpVolume() }
            } label: {
                ButtonLabel(
                    title: String(localized: "Back up now"), systemImage: "icloud.and.arrow.up",
                    busy: running.contains(Self.volumeBackupKey) || volume.backingUp)
            }
            .buttonStyle(.bordered)
            .disabled(!canAct || running.contains(Self.volumeBackupKey) || volume.backingUp)
        }
        .controlSize(.regular)
    }

    private func snapshotRow(_ snapshot: LonghornSnapshot) -> some View {
        let busy = running.contains(snapshot.name)
        return LonghornSnapshotRow(snapshot: snapshot, busy: busy)
            .swipeActions(edge: .trailing) {
                if canAct && !busy {
                    if snapshot.volumeSnapshot.isEmpty {
                        Button(role: .destructive) {
                            confirmDelete = snapshot
                        } label: {
                            Label("Delete", systemImage: "trash")
                        }
                    }
                    if !snapshot.backedUp && snapshot.ready {
                        Button {
                            Task { await run(.backup, snapshot: snapshot.name) }
                        } label: {
                            Label("Back up", systemImage: "icloud.and.arrow.up")
                        }
                        .tint(.accentColor)
                    }
                }
            }
            .contextMenu {
                if canAct && !busy {
                    if !snapshot.backedUp && snapshot.ready {
                        Button {
                            Task { await run(.backup, snapshot: snapshot.name) }
                        } label: {
                            Label("Back up", systemImage: "icloud.and.arrow.up")
                        }
                    }
                    if snapshot.volumeSnapshot.isEmpty {
                        Button(role: .destructive) {
                            confirmDelete = snapshot
                        } label: {
                            Label("Delete", systemImage: "trash")
                        }
                    } else {
                        // Its VolumeSnapshot would be left unrestorable.
                        Text("It keeps the VolumeSnapshot \(snapshot.volumeSnapshot): delete that one instead.")
                    }
                }
            }
    }

    private func run(_ action: LonghornSnapshotAction, snapshot: String) async {
        guard let client = model.client, !running.contains(snapshot) else { return }
        running.insert(snapshot)
        defer { running.remove(snapshot) }
        do {
            try await client.longhornSnapshotAction(
                namespace: volume.namespace, volume: volume.name, snapshot: snapshot, action: action)
            resultMessage =
                switch action {
                case .take: String(localized: "Snapshot of \(volume.label) taken")
                case .backup: String(localized: "Backup of \(volume.label) started")
                case .delete: String(localized: "Snapshot of \(volume.label) deleted")
                }
            await refresh()
        } catch {
            resultMessage = String(localized: "Could not change \(volume.label): \(error.localizedDescription)")
        }
    }

    /// A new snapshot backed up at once: the volume's own backup action.
    private func backUpVolume() async {
        guard let client = model.client, !running.contains(Self.volumeBackupKey) else { return }
        running.insert(Self.volumeBackupKey)
        defer { running.remove(Self.volumeBackupKey) }
        do {
            try await client.longhornAction(namespace: volume.namespace, name: volume.name, action: .backup)
            resultMessage = String(localized: "Backup of \(volume.label) started")
            await refresh()
        } catch {
            resultMessage = String(localized: "Could not change \(volume.label): \(error.localizedDescription)")
        }
    }
}

/// A button's icon and title, a spinner in place of the icon while it runs.
private struct ButtonLabel: View {
    let title: String
    let systemImage: String
    let busy: Bool

    var body: some View {
        HStack(spacing: 6) {
            if busy { ProgressView().controlSize(.small) } else { Image(systemName: systemImage) }
            Text(verbatim: title).lineLimit(1)
        }
        .frame(maxWidth: .infinity)
    }
}

private struct LonghornStatTile: View {
    let label: String
    let value: String
    let caption: String?
    var accent: Color?

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: label).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
            Text(verbatim: value).font(.headline).foregroundStyle(accent ?? .primary).lineLimit(1).minimumScaleFactor(
                0.7)
            if let caption {
                Text(verbatim: caption).font(.caption2).foregroundStyle(accent ?? .secondary).lineLimit(2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background((accent ?? .accentColor).opacity(0.1), in: RoundedRectangle(cornerRadius: 12))
        .accessibilityElement(children: .combine)
    }
}

/// A round tinted badge holding a symbol.
private struct SymbolBadge: View {
    let systemImage: String
    let color: Color

    var body: some View {
        Image(systemName: systemImage)
            .font(.subheadline)
            .foregroundStyle(color)
            .frame(width: 34, height: 34)
            .background(color.opacity(0.14), in: Circle())
            .accessibilityHidden(true)
    }
}

private struct LonghornSnapshotRow: View {
    let snapshot: LonghornSnapshot
    let busy: Bool

    var body: some View {
        HStack(spacing: 12) {
            SymbolBadge(systemImage: snapshot.userCreated ? "camera" : "clock.arrow.circlepath", color: .accentColor)
            VStack(alignment: .leading, spacing: 3) {
                HStack {
                    Text(verbatim: snapshot.createdAt > 0 ? relativeTime(snapshot.createdAt) : snapshot.name).font(
                        .subheadline
                    ).lineLimit(1)
                    Spacer()
                    if busy { ProgressView() }
                }
                Text(verbatim: details).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                    .truncationMode(.middle)
                HStack(spacing: 6) {
                    if snapshot.backedUp {
                        StatusPill(label: String(localized: "backed up"), color: .statusOK)
                    } else {
                        StatusPill(label: String(localized: "local only"), color: .secondary)
                    }
                    if !snapshot.ready { StatusPill(label: String(localized: "not ready"), color: .statusWarn) }
                }
                if !snapshot.volumeSnapshot.isEmpty {
                    StatusPill(
                        label: String(localized: "VolumeSnapshot \(snapshot.volumeSnapshot)"), color: .accentColor)
                }
                if !snapshot.error.isEmpty {
                    Text(verbatim: snapshot.error).font(.caption).foregroundStyle(.statusBad)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var details: String {
        [
            snapshot.name, formatBytes(snapshot.size),
            snapshot.userCreated ? nil : String(localized: "taken by Longhorn"),
        ]
        .compactMap { $0 }.joined(separator: " · ")
    }
}

private struct LonghornBackupRow: View {
    let backup: LonghornBackup

    var body: some View {
        HStack(spacing: 12) {
            SymbolBadge(systemImage: symbol, color: color)
            VStack(alignment: .leading, spacing: 3) {
                HStack {
                    Text(verbatim: backup.createdAt > 0 ? relativeTime(backup.createdAt) : backup.name).font(
                        .subheadline
                    ).lineLimit(1)
                    Spacer()
                    StatusPill(label: state, color: color)
                }
                Text(verbatim: details).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                    .truncationMode(.middle)
                if !backup.completed && !backup.failed {
                    ProgressView(value: Double(min(100, max(0, backup.progress))), total: 100)
                }
                if !backup.error.isEmpty {
                    Text(verbatim: backup.error).font(.caption).foregroundStyle(.statusBad)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var symbol: String {
        backup.failed
            ? "exclamationmark.icloud" : backup.completed ? "checkmark.icloud" : "arrow.triangle.2.circlepath.icloud"
    }

    private var color: Color { backup.failed ? Color.statusBad : backup.completed ? Color.statusOK : Color.accentColor }

    private var state: String {
        backup.failed
            ? String(localized: "failed")
            : backup.completed ? String(localized: "completed") : String(localized: "in progress")
    }

    private var details: String {
        [
            backup.size > 0 ? formatBytes(backup.size) : nil,
            backup.snapshot.isEmpty ? nil : String(localized: "from \(backup.snapshot)"),
        ]
        .compactMap { $0 }.joined(separator: " · ")
    }
}
