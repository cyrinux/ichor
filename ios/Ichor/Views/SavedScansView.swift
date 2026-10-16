import SwiftUI
import IchorCore

/// The vulnerability reports kept on this phone for `cluster` (encrypted, 90 days), newest
/// first: app, date, scanner, severities and what was new. One opens (read from the phone,
/// nothing runs in the cluster), is swiped away, or all are cleared.
struct SavedScansView: View {
    let cluster: String

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[SavedScan]> = .loading
    @State private var confirmClear = false

    var body: some View {
        content
            .themedBackground()
            .navigationTitle("Scan reports")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if !scans.isEmpty {
                    ToolbarItem(placement: .primaryAction) {
                        Button(role: .destructive) { confirmClear = true } label: {
                            Image(systemName: "trash").accessibilityLabel(Text("Clear all"))
                        }
                    }
                }
            }
            .confirmationDialog(String(localized: "Clear the scan reports?"), isPresented: $confirmClear, titleVisibility: .visible) {
                Button(String(localized: "Clear all"), role: .destructive) { Task { await clear() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Every scan report kept on this phone for this cluster is deleted.")
            }
            .task { await load() }
            .refreshable { await load() }
    }

    private var scans: [SavedScan] {
        if case .loaded(let value, _, _) = state { return value }
        return []
    }

    /// App names from the inventory already loaded: the core keeps only the app id.
    private var names: [String: String] {
        let apps = model.lastKnown(.inventory, as: ClusterInventory.self)?.value.apps ?? []
        return Dictionary(apps.map { ($0.id, $0.name) }, uniquingKeysWith: { first, _ in first })
    }

    private func name(of scan: SavedScan) -> String {
        if let node = systemImagesNode(scan.app) { return String(localized: "System images · \(node)") }
        return names[scan.app] ?? scan.app
    }

    @ViewBuilder
    private var content: some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Request failed", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Retry") { Task { await load() } }
            }
        case .loaded(let all, _, _):
            List {
                ForEach(all) { scan in
                    let title = name(of: scan)
                    NavigationLink {
                        SavedScanReportView(saved: scan, appName: title)
                    } label: {
                        SavedScanRow(scan: scan, name: title)
                    }
                    .swipeActions {
                        Button(role: .destructive) { Task { await forget(scan) } } label: {
                            Label("Forget", systemImage: "trash")
                        }
                    }
                }
            }
            .overlay {
                if all.isEmpty {
                    ContentUnavailableView {
                        Label("Scan reports", systemImage: "checkmark.shield")
                    } description: {
                        Text("No scan report kept for this cluster yet. Completed scans are kept on this phone, encrypted, for 90 days.")
                    }
                }
            }
        }
    }

    private func load() async {
        state = await .from { try await TalosClient.imageScanHistory(cluster: cluster) }
    }

    private func forget(_ scan: SavedScan) async {
        try? await TalosClient.imageScanForget(id: scan.id)
        await load()
    }

    private func clear() async {
        try? await TalosClient.imageScanClear(cluster: cluster)
        await load()
    }
}

private struct SavedScanRow: View {
    let scan: SavedScan
    let name: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: name).lineLimit(1)
            Text(verbatim: [dateTimeText(scan.finished), scan.scanner].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.caption).foregroundStyle(.secondary)
            SeverityChips(summary: scan.summary)
            NewFindingsLine(summary: scan.newSummary, hasPrevious: false)
        }
    }
}

/// A kept report, read from the phone when opened, then shown like a fresh one.
struct SavedScanReportView: View {
    let saved: SavedScan
    let appName: String

    @State private var loaded: (report: ImageScanReport, json: String)?
    @State private var failure: String?

    var body: some View {
        Group {
            if let loaded {
                ImageScanReportView(report: loaded.report, reportJSON: loaded.json, appName: appName)
            } else if let failure {
                ContentUnavailableView {
                    Label("Vulnerability report", systemImage: "exclamationmark.triangle")
                } description: {
                    Text("Could not open the report: \(failure)")
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .task(id: saved.id) {
            do {
                loaded = try await TalosClient.imageScanSaved(id: saved.id)
            } catch {
                failure = error.localizedDescription
            }
        }
    }
}
