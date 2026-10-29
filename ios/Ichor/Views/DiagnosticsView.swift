import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// Settings → Support → Export diagnostics: MetricKit's crash and hang reports, then the newest
/// entries of the core's redacted log, exported as a zip the user shares or saves themself.
struct DiagnosticsView: View {
    @State private var records: [CrashRecord] = []
    @State private var entries: [DiagEntry] = []
    @State private var loaded = false
    @State private var loadError: String?
    @State private var exported: DiagExportSummary?
    @State private var exporting = false
    @State private var exportError: String?
    @State private var confirmClear = false

    var body: some View {
        content
            .themedBackground()
            .navigationTitle("Diagnostics")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        Task { await export() }
                    } label: {
                        Image(systemName: "square.and.arrow.up").accessibilityLabel(Text("Export diagnostics"))
                    }
                    .disabled(exporting)
                }
                if !entries.isEmpty {
                    ToolbarItem(placement: .primaryAction) {
                        Button(role: .destructive) {
                            confirmClear = true
                        } label: {
                            Image(systemName: "trash").accessibilityLabel(Text("Clear log"))
                        }
                    }
                }
            }
            .confirmationDialog(
                String(localized: "Clear the diagnostics log?"), isPresented: $confirmClear, titleVisibility: .visible
            ) {
                Button(String(localized: "Clear log"), role: .destructive) { Task { await clear() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(
                    "Every entry of the log is deleted, and its file on this device if it is kept. The system's crash reports stay."
                )
            }
            .task { await load() }
            .refreshable { await load() }
    }

    @ViewBuilder
    private var content: some View {
        if !loaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if isEmpty {
            ContentUnavailableView {
                Label("Diagnostics", systemImage: "stethoscope")
            } description: {
                Text("No crash report and no log entry yet.")
            }
        } else {
            list
        }
    }

    private var list: some View {
        List {
            exportSection
            Section {
                if records.isEmpty {
                    Text("The system kept no crash or hang report for the app. iOS hands one over at a later launch.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                ForEach(Array(records.enumerated()), id: \.offset) { _, record in
                    CrashRecordRow(record: record)
                }
            } header: {
                Text("Crash and hang reports")
            }
            Section {
                if let loadError {
                    Text(verbatim: loadError).font(.footnote).foregroundStyle(Color.statusBad)
                } else if entries.isEmpty {
                    Text("The log is empty.").font(.footnote).foregroundStyle(.secondary)
                }
                ForEach(Array(entries.enumerated()), id: \.offset) { _, entry in
                    DiagEntryRow(entry: entry)
                }
            } header: {
                Text("Log")
            } footer: {
                Text(
                    "The zip holds the app version and device model, this log, the system's crash reports and the actions the app made, all redacted: no secret, cluster name, address or namespace. Nothing is sent: you share or save it yourself."
                )
            }
        }
    }

    /// Nothing to show and no zip on the way.
    private var isEmpty: Bool {
        records.isEmpty && entries.isEmpty && loadError == nil && exported == nil && exportError == nil && !exporting
    }

    @ViewBuilder
    private var exportSection: some View {
        if exporting || exported != nil || exportError != nil {
            Section {
                if exporting {
                    Label("Making the zip…", systemImage: "hourglass")
                }
                if let exportError {
                    Text("Could not make the diagnostics zip: \(exportError)")
                        .font(.footnote).foregroundStyle(Color.statusBad)
                }
                if let exported {
                    let url = URL(fileURLWithPath: exported.path)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: url.lastPathComponent)
                        Text(verbatim: exported.bytes.formatted(.byteCount(style: .file)))
                            .font(.caption).foregroundStyle(.secondary)
                        Text("Log entries: \(exported.entries)").font(.caption).foregroundStyle(.secondary)
                    }
                    LocalFileActions(
                        url: url, contentType: .zip, deleteTitle: Text("Delete the diagnostics zip?"),
                        onDelete: { self.exported = nil })
                }
            }
        }
    }

    private func load() async {
        records = await Task.detached { CrashDiagnostics.records() }.value
        do {
            entries = try await TalosClient.diagEntries(limit: DiagnosticsFiles.previewEntries)
            loadError = nil
        } catch {
            loadError = error.localizedDescription
        }
        loaded = true
    }

    private func export() async {
        exporting = true
        exportError = nil
        exported = nil
        do {
            exported = try await TalosClient.diagExport()
        } catch {
            exportError = error.localizedDescription
        }
        exporting = false
    }

    private func clear() async {
        try? await TalosClient.diagClear()
        await load()
    }
}

private struct CrashRecordRow: View {
    let record: CrashRecord

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Label {
                Text(title)
            } icon: {
                Image(systemName: record.kind == .crash ? "exclamationmark.octagon" : "exclamationmark.triangle")
                    .foregroundStyle(record.kind == .crash ? Color.statusBad : Color.statusWarn)
            }
            Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
            if !record.terminationReason.isEmpty {
                Text(verbatim: record.terminationReason).font(.caption)
            }
            if !record.codesLine.isEmpty {
                Text(verbatim: record.codesLine).font(.caption).foregroundStyle(.secondary)
            }
            if !record.frames.isEmpty {
                DisclosureGroup("Frames") {
                    Text(verbatim: record.frames.joined(separator: "\n"))
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                }
                .font(.caption)
            }
        }
    }

    private var title: String {
        switch record.kind {
        case .crash: String(localized: "Crash")
        case .hang: String(localized: "Hang")
        case .cpuException: String(localized: "CPU exception")
        case .diskWriteException: String(localized: "Disk-write exception")
        case .launch: String(localized: "Slow launch")
        }
    }

    private var details: String {
        [
            [record.begin, record.end].filter { !$0.isEmpty }.joined(separator: " – "), record.appVersion,
            record.osVersion,
        ]
        .filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

private struct DiagEntryRow: View {
    let entry: DiagEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: entry.msg)
                .font(.callout)
                .foregroundStyle(entry.isProblem ? Color.statusWarn : Color.primary)
            Text(verbatim: header).font(.caption).foregroundStyle(.secondary)
            if !entry.attrs.isEmpty {
                Text(verbatim: entry.attrsLine).font(.caption.monospaced()).foregroundStyle(.secondary)
            }
            if !entry.err.isEmpty {
                Text(verbatim: entry.err).font(.caption).foregroundStyle(Color.statusBad)
            }
        }
    }

    private var header: String {
        [entry.date.formatted(date: .abbreviated, time: .standard), entry.level, entry.component]
            .joined(separator: " · ")
    }
}
