import SwiftUI
import IchorCore

/// Upgrades a Longhorn volume's engine: live when it is attached, offline when it is detached.
/// Lists the engine images the core offers (the default preselected), says why Longhorn refuses,
/// and asks the volume's name before confirming.
struct LonghornEngineSheet: View {
    let volume: LonghornVolume
    let onConfirm: (String) -> Void
    @Environment(AppModel.self) private var model
    @Environment(\.kubeActionAccess) private var access
    @Environment(\.dismiss) private var dismiss
    @State private var preview: LonghornEngineUpgradePreview?
    @State private var selected = ""
    @State private var typed = ""
    @State private var error: String?

    init(volume: LonghornVolume, onConfirm: @escaping (String) -> Void) {
        self.volume = volume
        self.onConfirm = onConfirm
    }

    var body: some View {
        let denial = access?.denial(for: .longhornEngineUpgrade, in: volume.namespace)
        NavigationStack {
            Form {
                if preview == nil && error == nil { ProgressView() }
                if let error { ErrorLine(error: error) }
                if let p = preview {
                    Section {
                        if p.live {
                            Text("Live upgrade: the volume stays attached, and its replicas double for a moment.").foregroundStyle(.statusWarn)
                        } else {
                            Text("Offline upgrade: the volume is detached, so the engine is replaced before its next attach.")
                        }
                        LabeledContent {
                            Text(verbatim: p.currentImage.or("—")).monospaced().lineLimit(2)
                        } label: {
                            Text("Current engine")
                        }
                        if !p.error.isEmpty { ErrorLine(error: p.error) }
                        if !p.canUpgrade && !p.reason.isEmpty { Text(verbatim: p.reason).foregroundStyle(.statusWarn) }
                    }
                    Section("Engine image") {
                        ForEach(p.images) { image in
                            Button { selected = image.image } label: { imageRow(image, current: p.currentImage) }
                                .disabled(!image.selectable || image.image == p.currentImage)
                        }
                    }
                }
                Section("Type \(volume.name) to confirm") {
                    TextField("Volume name", text: $typed).autocorrectionDisabled().textInputAutocapitalization(.never)
                    Button { onConfirm(selected) } label: { Text("Upgrade engine") }
                        .disabled(preview?.accepts(selected) != true || !typedConfirmationMatches(typed, token: volume.name) || denial != nil)
                    KubeDeniedNote(.longhornEngineUpgrade, in: volume.namespace)
                }
            }
            .navigationTitle(String(localized: "Upgrade engine"))
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
        }
        .task(id: volume.id) {
            guard let client = model.client else { error = String(localized: "Not connected"); return }
            do {
                let p = try await client.longhornEngineUpgradePreview(namespace: volume.namespace, name: volume.name)
                preview = p
                selected = p.preselected
            } catch { self.error = error.localizedDescription }
        }
    }

    private func imageRow(_ image: LonghornEngineImage, current: String) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: image.image).monospaced().lineLimit(2)
                if !image.version.isEmpty {
                    Text(verbatim: image.version).font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer()
            if image.isDefault { Tag(String(localized: "default")) }
            if image.incompatible { Tag(String(localized: "incompatible"), color: .statusBad) }
            if selected == image.image { Image(systemName: "checkmark") }
        }
    }
}
