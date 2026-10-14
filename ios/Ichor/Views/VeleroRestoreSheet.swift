import SwiftUI
import IchorCore

/// A Velero backup to restore: its namespace and name.
struct VeleroRestoreTarget: Identifiable, Hashable {
    let namespace: String
    let name: String
    var id: String { "\(namespace)/\(name)" }
}

/// Restoring a backup: what it holds first, then the namespaces to restore (all by default,
/// each into another namespace if wanted), how existing objects are treated and whether volumes
/// are restored, confirmed by typing the backup's name. After the launch the sheet follows the
/// Restore's phase and item progress until it ends.
struct VeleroRestoreSheet: View {
    let target: VeleroRestoreTarget
    /// Reloads the data services, after the restore went through or ended.
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.kubeActionAccess) private var access
    @Environment(\.dismiss) private var dismiss
    @State private var preview: VeleroRestorePreview?
    @State private var loadError: String?
    @State private var selected: Set<String> = []
    @State private var targets: [String: String] = [:]
    @State private var updateExisting = false
    @State private var restoreVolumes = true
    @State private var typed = ""
    @State private var restoreName: String?
    @State private var launchError: String?

    private var denial: KubeAccess? { access?.denial(for: .veleroRestore, in: target.namespace) }
    private var allNamespaces: Bool {
        let included = preview?.includedNamespaces ?? []
        return included.isEmpty || included.contains("*")
    }
    private var options: VeleroRestoreOptions {
        VeleroRestoreOptions(
            includedNamespaces: allNamespaces ? [] : Array(selected),
            namespaceMapping: targets.filter { selected.contains($0.key) && !$0.value.isEmpty && $0.value != $0.key },
            existingResourcePolicy: updateExisting ? "update" : "none",
            restorePVs: restoreVolumes ? nil : false,
        )
    }
    private var restore: VeleroRestoreDetail? {
        preview?.restores.first { $0.name == restoreName }
    }

    var body: some View {
        NavigationStack {
            Form {
                if restoreName != nil || launchError != nil {
                    progress
                } else {
                    choices
                }
            }
            .navigationTitle(String(localized: "Restore backup \(target.name)?"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
        }
        .task(id: target.id) { await load() }
        .task(id: restoreName) { await follow() }
    }

    /// The preview loads once; after the launch the Restore's progress is polled every 5 s
    /// until it ends.
    private func load() async {
        guard let client = model.client else { loadError = String(localized: "Not connected"); return }
        do {
            preview = try await client.veleroRestorePreview(namespace: target.namespace, backup: target.name)
            selected = Set((preview?.includedNamespaces ?? []).filter { $0 != "*" })
        } catch {
            if !Task.isCancelled { loadError = error.localizedDescription }
        }
    }

    private func follow() async {
        guard restoreName != nil else { return }
        while !Task.isCancelled {
            if let client = model.client {
                preview = try? await client.veleroRestorePreview(namespace: target.namespace, backup: target.name)
            }
            // The Restore exists as soon as the launch answered; when it cannot be found the
            // launch failed and the error shows.
            if let restore, restore.finished { break }
            if restore == nil { break }
            try? await Task.sleep(for: .seconds(5))
        }
        await refresh()
    }

    private func launch() async {
        guard let client = model.client else { return }
        do {
            restoreName = try await client.veleroRestore(namespace: target.namespace, backup: target.name, options: options)
        } catch {
            launchError = error.localizedDescription
        }
    }

    // MARK: Form

    @ViewBuilder private var choices: some View {
        Section {
            Text("Restoring creates the backup's objects again. Target namespaces that do not exist are created; ones that exist get the backup's objects merged in, overwriting them with update.").foregroundStyle(.statusWarn)
        }
        if preview == nil && loadError == nil { Section { ProgressView() } }
        if let loadError { Section { ErrorLine(error: loadError) } }
        if let p = preview {
            Section {
                if !p.error.isEmpty { ErrorLine(error: p.error) }
                holds(p)
                if !p.canRestore {
                    ForEach(p.reasons, id: \.self) { reason in
                        Text(restoreReasonText(reason)).foregroundStyle(.red)
                    }
                }
            }
            if p.canRestore {
                Section {
                    if allNamespaces {
                        Text("Every namespace in the backup is restored.")
                    } else {
                        Text("Choose the namespaces to restore")
                        ForEach(p.includedNamespaces.filter { $0 != "*" }, id: \.self) { ns in
                            VStack(alignment: .leading, spacing: 4) {
                                Toggle(ns, isOn: Binding(
                                    get: { selected.contains(ns) },
                                    set: { selected = $0 ? selected.union([ns]) : selected.subtracting([ns]) }))
                                if selected.contains(ns) {
                                    TextField(String(localized: "empty: same name"), text: Binding(
                                        get: { targets[ns] ?? "" },
                                        set: { targets[ns] = $0.trimmingCharacters(in: .whitespaces) }))
                                        .font(.callout.monospaced())
                                        .autocorrectionDisabled()
                                        .textInputAutocapitalization(.never)
                                }
                            }
                        }
                    }
                }
                Section {
                    Picker("Existing objects", selection: $updateExisting) {
                        Text("leave").tag(false)
                        Text("update (overwrite)").tag(true)
                    }
                    Picker("Restore volumes", selection: $restoreVolumes) {
                        Text("as backed up").tag(true)
                        Text("no").tag(false)
                    }
                }
            }
        }
        Section("Type \(target.name) to confirm") {
            TextField("Backup name", text: $typed).autocorrectionDisabled().textInputAutocapitalization(.never)
            Button(role: .destructive) { Task { await launch() } } label: { Text("Restore") }
                .disabled(preview?.canRestore != true || !typedConfirmationMatches(typed, token: target.name) || denial != nil)
            KubeDeniedNote(.veleroRestore, in: target.namespace)
        }
    }

    @ViewBuilder private func holds(_ p: VeleroRestorePreview) -> some View {
        LabeledContent("Phase", value: [p.phase, p.completedAt > 0 ? relativeTime(p.completedAt) : nil, veleroRestoreCountsText(errors: p.errors, warnings: p.warnings)].compactMap { $0 }.joined(separator: " · "))
        LabeledContent("Namespaces", value: p.includedNamespaces.filter { $0 != "*" }.isEmpty ? String(localized: "all") : p.includedNamespaces.filter { $0 != "*" }.joined(separator: ", "))
        LabeledContent("Items", value: "\(p.totalItems)")
        LabeledContent("Volume snapshots", value: p.volumeSnapshots == 0 ? String(localized: "no volume snapshots") : "\(p.volumeSnapshots)")
        if !p.storageLocation.isEmpty { LabeledContent("Storage location", value: p.storageLocation) }
    }

    // MARK: Progress

    @ViewBuilder private var progress: some View {
        Section {
            if let launchError { ErrorLine(error: launchError) }
            if let restore {
                LabeledContent("Restore", value: restore.name)
                LabeledContent("Phase", value: [restore.phase, veleroRestoreCountsText(errors: restore.errors, warnings: restore.warnings)].compactMap { $0 }.joined(separator: " · "))
                if restore.totalItems > 0 {
                    LabeledContent("Progress", value: String(localized: "\(restore.itemsRestored) of \(restore.totalItems) items"))
                }
                ForEach(restore.validationErrors, id: \.self) { Text(verbatim: $0).font(.caption).foregroundStyle(.red) }
                if !restore.failureReason.isEmpty { Text(verbatim: restore.failureReason).font(.caption).foregroundStyle(.red) }
                if restore.running {
                    HStack(spacing: 8) { ProgressView(); Text("restoring…").font(.caption).foregroundStyle(.secondary) }
                }
            } else if launchError == nil {
                HStack(spacing: 8) { ProgressView(); Text("Restore \(restoreName ?? "") started") }
            }
        }
    }

    private func restoreReasonText(_ reason: VeleroRestoreReason) -> String {
        switch reason {
        case .partiallyFailed: String(localized: "The backup completed with errors: some items may be missing")
        case .locationUnavailable: String(localized: "The backup's storage location is unavailable: its data cannot be read")
        case .expired: String(localized: "The backup has expired: its data is gone")
        case .restoreRunning: String(localized: "A restore of this backup is already running")
        case .allNamespaces: String(localized: "This backup holds every namespace: restoring it restores them all")
        }
    }
}

/// "2 errors, 3 warnings", nil when both are 0.
private func veleroRestoreCountsText(errors: Int, warnings: Int) -> String? {
    let parts = [errors > 0 ? String(localized: "\(errors) errors") : nil,
                 warnings > 0 ? String(localized: "\(warnings) warnings") : nil].compactMap { $0 }
    return parts.isEmpty ? nil : parts.joined(separator: ", ")
}
