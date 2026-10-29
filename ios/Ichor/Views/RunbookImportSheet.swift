import SwiftUI
import UniformTypeIdentifiers
import IchorCore

/// Imports a runbook: pasted, picked in Files or downloaded from an https URL. Check has the
/// core load it and plan it against the cluster, storing nothing; the plan (each step, its
/// target, destructive or not, its role, whether the cluster allows it) is shown, and only
/// "Keep runbook" stores it. Nothing runs on import. Same flow as Android's RunbookImportSheet.kt.
struct RunbookImportSheet: View {
    let fingerprint: String

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
    @Environment(AppModel.self) private var model
    @State private var source: Source = .paste
    @State private var yaml = ""
    @State private var url = ""
    @State private var busy = false
    @State private var failure: String?
    @State private var pickingFile = false
    /// The file checked and its plan: nothing is stored until Keep.
    @State private var checked: (yaml: String, plan: RunbookPlan)?

    var body: some View {
        NavigationStack {
            Form {
                if let checked {
                    RunbookPlanSections(plan: checked.plan, continueIDs: .constant([]), editable: false)
                    Section {
                        Text(
                            "Keeping it runs nothing: you run it from its plan, one confirmation per destructive step."
                        )
                        .foregroundStyle(.secondary)
                    }
                    failureSection
                } else {
                    importSections
                }
            }
            .navigationTitle(checked.map { Text(verbatim: $0.plan.name) } ?? Text("Import runbook"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(busy)
                }
                if let checked {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Keep runbook") { Task { await keep(checked.yaml) } }.disabled(busy)
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
                    // The core names the step: kept as it is.
                    Text(verbatim: failure).font(.system(.footnote, design: .monospaced)).foregroundStyle(.red)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    @ViewBuilder private var importSections: some View {
        Section {
            Picker("Import runbook", selection: $source) {
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
                    .accessibilityLabel(Text("Runbook YAML"))
                Button("Paste from the clipboard") {
                    if let text = UIPasteboard.general.string { yaml = text }
                }
                Button("Check the plan") { Task { await check(yaml) } }
                    .disabled(busy || yaml.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            case .file:
                Button("Choose a file") { pickingFile = true }
                    .disabled(busy)
                    .fileImporter(isPresented: $pickingFile, allowedContentTypes: [.yaml, .plainText, .data, .item]) {
                        result in
                        switch result {
                        case .success(let url): readFile(url)
                        case .failure(let error): failure = error.localizedDescription
                        }
                    }
            case .url:
                TextField("https:// address of the runbook file", text: $url)
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

    /// Loads the runbook (its errors name the step), then plans it on the cluster on screen.
    private func check(_ text: String) async {
        busy = true
        failure = nil
        defer { busy = false }
        do {
            _ = try await TalosClient.runbookSummary(yaml: text)
            guard let client = model.client else {
                failure = String(localized: "the cluster cannot be reached now.")
                return
            }
            checked = (text, try await client.runbookPlan(yaml: text))
        } catch {
            failure = error.localizedDescription
        }
    }

    private func download() async {
        busy = true
        failure = nil
        do {
            let text = try await TalosClient.downloadRunbook(url: url.trimmingCharacters(in: .whitespaces))
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
            // The core refuses more than 64 KiB; a larger file is not read at all.
            guard data.count <= 64 * 1024, let text = String(data: data, encoding: .utf8) else {
                failure = String(localized: "The file is larger than 64 KiB.")
                return
            }
            yaml = text
            Task { await check(text) }
        } catch {
            failure = error.localizedDescription
        }
    }

    private func keep(_ text: String) async {
        busy = true
        failure = nil
        defer { busy = false }
        do {
            try await RunbookStore.shared.importRunbook(text, fingerprint: fingerprint)
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
    }
}
