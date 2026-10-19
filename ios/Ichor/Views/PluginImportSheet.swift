import SwiftUI
import UniformTypeIdentifiers
import IchorCore

/// Imports a plugin file: pasted, picked in Files or downloaded from an https URL; Check has
/// the core validate it, storing nothing. The consent step then shows what it reads (a Secret
/// shown in clear in red) and can change; only Enable imports it, on. Cancel or closing the
/// sheet leaves the stored plugins as they were. Same flow as Android's PluginImportSheet.kt.
struct PluginImportSheet: View {
    private enum Source: String, CaseIterable, Identifiable {
        case paste, file, url
        var id: String { rawValue }

        var label: LocalizedStringKey {
            switch self {
            case .paste: "Paste"
            case .file: "File"
            case .url: "URL"
            }
        }
    }

    @Environment(\.dismiss) private var dismiss
    @State private var source: Source = .paste
    @State private var yaml = ""
    @State private var url = ""
    @State private var busy = false
    @State private var failure: String?
    @State private var pickingFile = false
    /// The file checked and what it showed: nothing is stored until Enable.
    @State private var checked: (yaml: String, item: PluginUserItem)?

    var body: some View {
        NavigationStack {
            Form {
                if let checked {
                    ConsentSections(consent: checked.item.consent)
                    failureSection
                } else {
                    importSections
                }
            }
            .navigationTitle(checked.map { Text("Enable \($0.item.title)?") } ?? Text("Import a plugin"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(busy)
                }
                if let checked {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Enable") { Task { await enable(checked.yaml) } }.disabled(busy)
                    }
                }
            }
        }
        .interactiveDismissDisabled(busy)
    }

    @ViewBuilder private var failureSection: some View {
        if let failure {
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Not imported:").foregroundStyle(.red)
                    // The core's problems are "plugin.yaml:<line>: …" lines: kept as they are.
                    Text(verbatim: failure).font(.system(.footnote, design: .monospaced)).foregroundStyle(.red)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    @ViewBuilder private var importSections: some View {
        Section {
            Picker("Plugins", selection: $source) {
                ForEach(Source.allCases) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            switch source {
            case .paste:
                TextEditor(text: $yaml)
                    .font(.system(.footnote, design: .monospaced))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .frame(minHeight: 160)
                    .accessibilityLabel(Text("Plugin YAML"))
                Button("Paste from the clipboard") {
                    if let text = UIPasteboard.general.string { yaml = text }
                }
                Button("Check and import") { Task { await check(yaml) } }
                    .disabled(busy || yaml.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            case .file:
                Button("Choose a file") { pickingFile = true }
                    .disabled(busy)
                    .fileImporter(isPresented: $pickingFile, allowedContentTypes: [.yaml, .plainText, .data, .item]) { result in
                        switch result {
                        case .success(let url): readFile(url)
                        case .failure(let error): failure = error.localizedDescription
                        }
                    }
            case .url:
                TextField("https:// address of the plugin file", text: $url)
                    .keyboardType(.URL)
                    .textContentType(.URL)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                Button("Download and check") { Task { await download() } }
                    .disabled(busy || url.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            if busy {
                HStack {
                    ProgressView()
                    Text("Checking…").foregroundStyle(.secondary)
                }
            }
        }
        failureSection
    }

    private func check(_ text: String) async {
        busy = true
        failure = nil
        defer { busy = false }
        do {
            checked = (text, try await TalosClient.checkPlugin(yaml: text))
        } catch {
            failure = error.localizedDescription
        }
    }

    private func download() async {
        busy = true
        failure = nil
        do {
            let text = try await TalosClient.downloadPlugin(url: url.trimmingCharacters(in: .whitespaces))
            yaml = text
            busy = false
            await check(text)
        } catch {
            busy = false
            failure = error.localizedDescription
        }
    }

    private func readFile(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= pluginMaxBytes, let text = String(data: data, encoding: .utf8) else {
                failure = String(localized: "The file is larger than 1 MiB.")
                return
            }
            yaml = text
            Task { await check(text) }
        } catch {
            failure = error.localizedDescription
        }
    }

    private func enable(_ text: String) async {
        busy = true
        failure = nil
        defer { busy = false }
        do {
            _ = try await TalosClient.importPlugin(yaml: text)
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// The consent step: what the plugin reads (a Secret shown in clear in red) and can change.
private struct ConsentSections: View {
    let consent: PluginConsent

    var body: some View {
        if !consent.website.isEmpty || consent.replaces {
            Section {
                if !consent.website.isEmpty {
                    Text(verbatim: consent.website).foregroundStyle(.secondary)
                }
                if consent.replaces {
                    Text("It replaces the imported plugin with the same id.").foregroundStyle(.orange)
                }
            }
        }
        Section("It reads") {
            ForEach(consent.resources, id: \.self) { r in
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: r.name).font(.system(.body, design: .monospaced))
                    if r.secret {
                        Group {
                            if r.revealed {
                                Text("Shows Secret values in clear").foregroundStyle(.red)
                            } else {
                                Text("Secret values stay masked").foregroundStyle(.secondary)
                            }
                        }
                        .font(.caption)
                    }
                }
            }
        }
        Section {
            if consent.readOnly {
                Text("Nothing: it only reads.").foregroundStyle(.secondary)
            }
            ForEach(consent.actions, id: \.self) { a in
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: a.label)
                    Text(verbatim: [a.verb, a.needs].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("It can change")
        } footer: {
            Text("It reads with your own Kubernetes access and never calls anything but the Kubernetes API. Turn it off or remove it at any time in Settings → Plugins.")
        }
    }
}
