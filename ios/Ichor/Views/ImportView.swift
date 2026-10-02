import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// Import a talosconfig from a file, pasted text or a QR code; validated by the Go core
/// and previewed before it is stored in the Keychain.
struct ImportView: View {
    var onImported: () -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(onImported: @escaping () -> Void = {}) {
        self.onImported = onImported
    }

    @Environment(AppModel.self) private var model
    @State private var source = Source.file
    @State private var pasted = ""
    @State private var showingImporter = false
    @State private var preview: (yaml: String, summary: ConfigSummary)?
    @State private var error: String?
    @State private var busy = false
    @State private var showingHelp = false

    enum Source: String, CaseIterable, Identifiable {
        case file = "File", paste = "Paste", qr = "QR code"
        var id: String { rawValue }

        var label: String {
            switch self {
            case .file: String(localized: "File")
            case .paste: String(localized: "Paste")
            case .qr: String(localized: "QR code")
            }
        }
    }

    var body: some View {
        Group {
            if let preview {
                PreviewList(summary: preview.summary, adding: model.yaml != nil, busy: busy, onCancel: { self.preview = nil }) {
                    Task { await save(preview.yaml) }
                }
            } else {
                picker
            }
        }
        .navigationTitle("Import talosconfig")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { showingHelp = true } label: { Image(systemName: "questionmark.circle") }
                    .accessibilityLabel("How to create a talosconfig")
            }
        }
        .sheet(isPresented: $showingHelp) { HelpSheet() }
        .fileImporter(isPresented: $showingImporter, allowedContentTypes: [.yaml, .plainText, .data, .item]) { result in
            switch result {
            case .success(let url): readFile(url)
            case .failure(let failure): error = failure.localizedDescription
            }
        }
    }

    private var picker: some View {
        VStack(spacing: 16) {
            VStack(spacing: 8) {
                Text("No cluster yet? Explore Ichor with a sample Talos cluster.")
                    .font(.callout).foregroundStyle(.secondary)
                Button("Try demo") {
                    busy = true
                    Task {
                        do { await save(try await TalosClient.demoConfig()) }
                        catch { self.error = error.localizedDescription; busy = false }
                    }
                }
                .buttonStyle(.bordered)
                .disabled(busy)
            }
            Picker("Source", selection: $source) {
                ForEach(Source.allCases) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)

            if let error { Text(error).foregroundStyle(.red).font(.footnote) }
            if busy { ProgressView() }

            switch source {
            case .file:
                Text("Select your talosconfig (~/.talos/config on your workstation), e.g. shared via AirDrop to Files.")
                    .font(.callout).foregroundStyle(.secondary)
                Button("Choose file") { showingImporter = true }.buttonStyle(.borderedProminent)
                Spacer()
            case .paste:
                TextEditor(text: $pasted)
                    .font(.system(.footnote, design: .monospaced))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .border(.quaternary)
                Button("Validate") { validate(pasted) }
                    .buttonStyle(.borderedProminent)
                    .disabled(pasted.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            case .qr:
                QRScannerView { validate($0) }
            }
        }
        .padding()
    }

    private func readFile(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= 256 * 1024, let text = String(data: data, encoding: .utf8) else {
                error = String(localized: "This file is not a talosconfig.")
                return
            }
            validate(text)
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func validate(_ yaml: String) {
        busy = true
        Task {
            defer { busy = false }
            do {
                preview = (yaml, try await TalosClient.parse(yaml))
                error = nil
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func save(_ yaml: String) async {
        busy = true
        defer { busy = false }
        do {
            try await model.save(yaml: yaml)
            onImported()
        } catch {
            self.error = error.localizedDescription
            preview = nil
        }
    }
}

private struct PreviewList: View {
    let summary: ConfigSummary
    /// A config is already stored: this one is added to it.
    let adding: Bool
    let busy: Bool
    let onCancel: () -> Void
    let onImport: () -> Void

    var body: some View {
        List {
            Section {
                Text("Config is valid").font(.headline)
            }
            ForEach(summary.contexts) { ctx in
                Section(ctx.name == summary.current ? String(localized: "\(ctx.name) (current)") : ctx.name) {
                    LabeledContent("Endpoints", value: ctx.endpoints.joined(separator: "\n"))
                    LabeledContent("Nodes", value: ctx.nodes.isEmpty ? String(localized: "endpoints") : "\(ctx.nodes.count)")
                    LabeledContent("Roles", value: ctx.roles.joined(separator: ", "))
                    LabeledContent("Cert expires", value: localizedCertExpiry(ctx.certNotAfter))
                }
            }
            if adding {
                Section {
                    Text("It is added to the clusters already on this device. A cluster imported before (same name and CA) is updated.")
                        .font(.footnote)
                }
            }
            Section {
                Text("Stored in the Keychain on this device only, never synced or backed up.")
                    .font(.footnote).foregroundStyle(.secondary)
                Button("Import", action: onImport).disabled(busy)
                Button("Cancel", role: .cancel, action: onCancel)
            }
        }
    }
}
