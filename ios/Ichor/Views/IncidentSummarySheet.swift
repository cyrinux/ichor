import SwiftUI
import UIKit
import IchorCore

/// "Share incident summary…" from the overview's title menu (CYR-77): the Markdown summary of
/// the chosen window, built by the core from this phone's history ring and action log, names
/// hidden by default. Shared as text through the system share sheet, or copied.
struct IncidentSummarySheet: View {
    /// The context name (the action log's key).
    let context: String
    /// The monitor key of its cluster (the history ring's key).
    let cluster: String

    @Environment(\.dismiss) private var dismiss
    @State private var window = IncidentWindow.standard
    @State private var hideNames = true
    @State private var state: LoadState<String> = .loading
    @State private var copied = 0

    var body: some View {
        NavigationStack {
            VStack(spacing: 12) {
                Picker("Window", selection: $window) {
                    ForEach(IncidentWindow.allCases) { option in
                        Text(verbatim: Self.title(of: option)).tag(option)
                    }
                }
                .pickerStyle(.segmented)
                Toggle("Hide names", isOn: $hideNames)
                preview
            }
            .padding()
            .themedBackground()
            .navigationTitle("Incident summary")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                if let markdown {
                    ToolbarItemGroup(placement: .bottomBar) {
                        Button {
                            UIPasteboard.general.string = markdown
                            copied += 1
                        } label: {
                            Label("Copy", systemImage: "doc.on.doc")
                        }
                        Spacer()
                        ShareLink(item: markdown) {
                            Label("Share", systemImage: "square.and.arrow.up")
                        }
                    }
                }
            }
            .sensoryFeedback(.success, trigger: copied)
            .task(id: LoadKey(window: window, hideNames: hideNames)) { await load() }
        }
        .presentationDragIndicator(.visible)
    }

    private var markdown: String? {
        if case .loaded(let text, _, _) = state { return text }
        return nil
    }

    @ViewBuilder
    private var preview: some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Request failed", systemImage: "exclamationmark.triangle")
            } description: {
                Text(verbatim: message)
            } actions: {
                Button("Retry") { Task { await load() } }
            }
        case .loaded(let text, _, _):
            ScrollView([.vertical, .horizontal]) {
                Text(verbatim: text)
                    .font(.footnote.monospaced())
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(10)
            }
            .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    private func load() async {
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        // The ring as the overview's banner reads it: unsealed here, nil before the first run
        // (the summary then says there is no recorded history).
        let ring = HistoryStore.bytes(cluster: cluster)
        let since = window.since(now: now)
        let anonymize = hideNames
        let loaded = await LoadState<String>.from {
            try await TalosClient.incidentSummary(context: context, ring: ring, since: since, now: now, anonymize: anonymize)
        }
        guard !Task.isCancelled else { return }
        state = loaded
    }

    private static func title(of window: IncidentWindow) -> String {
        switch window {
        case .sixHours: String(localized: "Last 6 hours")
        case .day: String(localized: "Last 24 hours")
        case .week: String(localized: "Last 7 days")
        }
    }

    /// What reloads the summary.
    private struct LoadKey: Equatable {
        let window: IncidentWindow
        let hideNames: Bool
    }
}
