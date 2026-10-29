import SwiftUI
import UniformTypeIdentifiers

/// Settings rows for the "This phone" provider: which model, its download (only on the
/// user's tap, on Wi-Fi unless they allow mobile data for this one download, never in Low
/// Data Mode), deleting it, or importing a model file instead.
struct LocalModelSection: View {
    @Environment(AISettings.self) private var ai
    @State private var allowMetered = false
    @State private var importing = false
    @State private var importError: String?
    @State private var confirmDelete = false

    private var store: LocalModelStore { .shared }
    private var downloads: ModelDownloads { .shared }

    var body: some View {
        let _ = downloads.version
        let selected = ai.effectiveModel
        if store.catalog.isEmpty {
            Text("This version offers no model to download. Import a model file (GGUF) instead.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        ForEach(store.catalog) { model in
            modelRow(
                id: model.id, title: model.name,
                detail: "\(Self.size(model.bytes)) · \(model.license)",
                fits: store.fits(model), selected: selected == model.id)
        }
        ForEach(store.imported(), id: \.self) { id in
            modelRow(
                id: id, title: String(id.dropFirst(LocalModelStore.importedPrefix.count)),
                detail: String(localized: "Imported file"), fits: true, selected: selected == id)
        }
        if let model = store.info(selected) {
            downloadRows(model)
        } else if selected.hasPrefix(LocalModelStore.importedPrefix) {
            deleteButton(selected)
        }
        Button("Import a model file") { importing = true }
            .fileImporter(isPresented: $importing, allowedContentTypes: [.data]) { result in
                importError = nil
                do {
                    let id = try store.importFile(from: result.get())
                    ai.setModel(id)
                    downloads.changed()
                } catch {
                    importError = error.localizedDescription
                }
            }
        if let importError { Text(verbatim: importError).font(.footnote).foregroundStyle(.statusBad) }
        Text(
            "The model answers on the phone: nothing is sent. A download starts only when you tap, on Wi-Fi unless you allow mobile data, never in Low Data Mode. A small model is often wrong: check its answer."
        )
        .font(.caption).foregroundStyle(.secondary)
    }

    private func modelRow(id: String, title: String, detail: String, fits: Bool, selected: Bool) -> some View {
        Button {
            ai.setModel(id)
            allowMetered = false
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: title).foregroundStyle(.primary)
                    Text(verbatim: detail).font(.caption).foregroundStyle(.secondary)
                    if !fits {
                        Text("Needs a phone with more memory").font(.caption).foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if selected { Image(systemName: "checkmark").foregroundStyle(.tint).accessibilityHidden(true) }
            }
        }
        .disabled(!fits)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    @ViewBuilder
    private func downloadRows(_ model: LocalModelInfo) -> some View {
        switch downloads.phases[model.id] {
        case .downloading(let done, let metered):
            VStack(alignment: .leading, spacing: 4) {
                ProgressView(value: Double(done), total: Double(max(model.bytes, 1)))
                Text("Downloading: \(Self.size(done)) of \(Self.size(model.bytes))").font(.footnote)
                switch DownloadWait.current(allowMetered: metered) {
                case .wifi: Text("Waiting for Wi-Fi.").font(.footnote).foregroundStyle(.secondary)
                case .lowDataMode: Text("Paused: Low Data Mode is on.").font(.footnote).foregroundStyle(.secondary)
                case nil: EmptyView()
                }
            }
            Button("Cancel", role: .cancel) { downloads.cancel(model.id) }
        case .verifying:
            HStack {
                ProgressView()
                Text("Checking the file…").font(.footnote)
            }
        case .failed(let message):
            Text(verbatim: message).font(.footnote).foregroundStyle(.statusBad)
            downloadButton(model)
        case nil:
            if store.file(for: model.id) != nil {
                Text("On this phone (\(Self.size(model.bytes))).").font(.footnote).foregroundStyle(.secondary)
                deleteButton(model.id)
            } else {
                downloadButton(model)
            }
        }
    }

    @ViewBuilder
    private func downloadButton(_ model: LocalModelInfo) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Toggle("Allow mobile data for this download", isOn: $allowMetered)
            Text("Off, the download waits for Wi-Fi. Never in Low Data Mode.")
                .font(.caption).foregroundStyle(.secondary)
        }
        Button("Download (\(Self.size(model.bytes)))") { downloads.start(model, allowMetered: allowMetered) }
        if let url = URL(string: model.licenseUrl) {
            Link("License: \(model.license)", destination: url)
        }
    }

    private func deleteButton(_ id: String) -> some View {
        Button("Delete the model", role: .destructive) { confirmDelete = true }
            .confirmationDialog("Delete the model", isPresented: $confirmDelete, titleVisibility: .visible) {
                Button("Delete the model", role: .destructive) {
                    downloads.delete(id)
                    // An imported file is gone for good: back to the catalog's choice.
                    if id.hasPrefix(LocalModelStore.importedPrefix) { ai.setModel("") }
                }
            } message: {
                Text("The model file is removed from the phone. Getting it back means downloading it again.")
            }
    }

    static func size(_ bytes: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
    }
}

/// Why the AI screens show no Ask button: no API key, or no model on the phone.
struct NotReadyHint: View {
    let onPhone: Bool

    var body: some View {
        Group {
            if onPhone {
                Text("To read the answer here, download or import a model in Settings.")
            } else {
                Text("To read the answer here, add an API key in Settings.")
            }
        }
        .font(.footnote).foregroundStyle(.secondary)
    }
}
