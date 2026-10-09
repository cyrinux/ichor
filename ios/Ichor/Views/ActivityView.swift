import SwiftUI
import IchorCore

/// What the app changed on `cluster` ("" for every cluster, with a cluster filter): newest
/// first, filtered by action or failures, shared as a Markdown table, or cleared.
struct ActivityView: View {
    let cluster: String
    @State private var state: LoadState<[ActivityEntry]> = .loading
    @State private var filter = ActivityFilter()
    @State private var export: String?
    @State private var confirmClear = false

    var body: some View {
        content
            .themedBackground()
            .navigationTitle("Activity")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if let export {
                    ToolbarItem(placement: .primaryAction) {
                        ShareLink(item: export) {
                            Image(systemName: "square.and.arrow.up").accessibilityLabel(Text("Share activity"))
                        }
                    }
                }
                if !entries.isEmpty {
                    ToolbarItem(placement: .primaryAction) {
                        Button(role: .destructive) { confirmClear = true } label: {
                            Image(systemName: "trash").accessibilityLabel(Text("Clear"))
                        }
                    }
                }
            }
            .confirmationDialog(String(localized: "Clear the activity log?"), isPresented: $confirmClear, titleVisibility: .visible) {
                Button(String(localized: "Clear"), role: .destructive) { Task { await clear() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("The recorded actions are deleted from this device. This cannot be undone.")
            }
            .task { await load() }
            .refreshable { await load() }
    }

    private var entries: [ActivityEntry] {
        if case .loaded(let value, _, _) = state { return value }
        return []
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
            list(all)
        }
    }

    private func list(_ all: [ActivityEntry]) -> some View {
        let rows = all.filter(filter.matches)
        return List {
            if !all.isEmpty {
                chips(all)
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
            }
            ForEach(Array(rows.enumerated()), id: \.offset) { _, entry in
                ActivityRow(entry: entry, showCluster: cluster.isEmpty)
            }
        }
        .overlay {
            if all.isEmpty {
                ContentUnavailableView {
                    Label("Activity", systemImage: "clock.arrow.circlepath")
                } description: {
                    Text("No action recorded yet. Every change Ichor makes to a cluster is listed here for 90 days, on this device only.")
                }
            } else if rows.isEmpty {
                ContentUnavailableView("No action matches these filters.", systemImage: "line.3.horizontal.decrease.circle")
            }
        }
    }

    private func chips(_ all: [ActivityEntry]) -> some View {
        let clusters = cluster.isEmpty ? all.distinctValues(\.cluster) : []
        let actions = all.distinctValues(\.action)
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                FilterChip(label: String(localized: "All"), count: all.count, selected: filter == ActivityFilter()) {
                    filter = ActivityFilter()
                }
                FilterChip(label: String(localized: "Failed"), count: all.filter(\.failed).count, selected: filter.failedOnly) {
                    filter.failedOnly.toggle()
                }
                // Only a choice when there is more than one.
                if clusters.count > 1 {
                    ForEach(clusters, id: \.self) { name in
                        FilterChip(label: name, count: all.filter { $0.cluster == name }.count, selected: filter.cluster == name) {
                            filter.cluster = filter.cluster == name ? nil : name
                        }
                    }
                }
                if actions.count > 1 {
                    ForEach(actions, id: \.self) { action in
                        FilterChip(label: ActivityEntry.label(of: action), count: all.filter { $0.action == action }.count,
                                   selected: filter.action == action) {
                            filter.action = filter.action == action ? nil : action
                        }
                    }
                }
            }
            .padding(.horizontal)
        }
        .scrollClipDisabled()
    }

    private func load() async {
        state = await .from { try await TalosClient.activity(cluster: cluster) }
        export = entries.isEmpty ? nil : try? await TalosClient.activityExport(cluster: cluster)
    }

    private func clear() async {
        try? await TalosClient.clearActivity(cluster: cluster)
        await load()
    }
}

private struct ActivityRow: View {
    let entry: ActivityEntry
    let showCluster: Bool

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: entry.failed ? "exclamationmark.circle" : "checkmark.circle")
                .foregroundStyle(entry.failed ? Color.statusBad : Color.statusOK)
                .accessibilityLabel(Text("Failed"))
                .accessibilityHidden(!entry.failed)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: [entry.actionLabel, entry.target].filter { !$0.isEmpty }.joined(separator: " · "))
                    .lineLimit(2)
                Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
                if !entry.params.isEmpty {
                    Text(verbatim: entry.params).font(.caption).foregroundStyle(.secondary)
                }
                if !entry.error.isEmpty {
                    Text(verbatim: entry.error).font(.caption).foregroundStyle(Color.statusBad)
                }
            }
        }
    }

    private var details: String {
        var parts = [entry.date.formatted(date: .abbreviated, time: .shortened)]
        if showCluster && !entry.cluster.isEmpty { parts.append(entry.cluster) }
        if entry.demo { parts.append(String(localized: "Demo")) }
        return parts.joined(separator: " · ")
    }
}
