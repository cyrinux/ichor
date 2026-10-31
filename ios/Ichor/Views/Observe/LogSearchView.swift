import SwiftUI
import IchorCore

/// Searches the cluster's log backend (Loki, VictoriaLogs; found in the cluster through the service
/// proxy, or at a URL set by hand): a query with a label picker, a time range, saved queries.
/// `initialQuery`, from a pod's log sheet, is searched at once over the last 24 hours.
struct LogSearchView: View {
    var initialQuery = ""

    @Environment(AppModel.self) private var model
    @State private var source: PromSource?
    @State private var resolved = false
    @State private var query = ""
    @State private var hours = logSearchDefaultHours
    /// Nil before the first search.
    @State private var result: LoadState<LogsResult>?
    @State private var saved: [String] = []
    @State private var searches = 0
    /// The latest search: an older one (the Go call ignores cancellation) never lands after it.
    @State private var generation = 0
    @State private var sourceOpen = false
    @State private var labelsOpen = false
    @State private var customOpen = false
    @State private var customHours = ""
    @State private var discovered: [PromSource]?
    @State private var discovering = false
    @State private var discoveryError: String?

    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }

    var body: some View {
        Group {
            if !resolved {
                SkeletonView()
            } else if let source {
                searchContent(source)
            } else {
                noSource
            }
        }
        .navigationTitle("Log search")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if case .loaded(let logs, _, _)? = result, !logs.lines.isEmpty {
                    ShareLink(item: logs.asText) { Label("Share", systemImage: "square.and.arrow.up") }
                }
                Button {
                    sourceOpen = true
                } label: {
                    Image(systemName: "slider.horizontal.3")
                }
                .accessibilityLabel(Text("Log backend"))
            }
        }
        .task(id: model.alertsKey) { await resolve() }
        .task(id: searches) { await search() }
        .sheet(isPresented: $sourceOpen) {
            SourceSheet(
                kind: .logs, current: source, discovered: discovered, discovering: discovering,
                discoveryError: discoveryError, discover: { await discover(autoSelect: false) }, test: test,
                save: setSource)
        }
        .sheet(isPresented: $labelsOpen) {
            if let source {
                LogSearchLabelsSheet(source: source) { label, value in
                    query = logSearchQuery(kind: source.kind, query, label: label, value: value)
                }
            }
        }
        .alert("Custom range", isPresented: $customOpen) {
            TextField(String(localized: "Hours back, up to \(logSearchMaxHours)"), text: $customHours)
                .keyboardType(.numberPad)
            Button("Search") {
                if let value = Int(customHours), (1...logSearchMaxHours).contains(value) { setHours(value) }
            }
            Button("Cancel", role: .cancel) {}
        }
        .themedBackground()
        // Reads go through the service proxy of the backend's namespace; a URL source asks nothing.
        .loadsKubeActionAccess(namespace: source?.mode == PromSource.proxy ? source?.namespace : nil)
    }

    private func searchContent(_ source: PromSource) -> some View {
        let example: String =
            source.kind == logsKindVictoriaLogs
            ? #"namespace:"demo" AND timeout"# : #"{namespace="demo"} |= "timeout""#
        return VStack(alignment: .leading, spacing: 8) {
            Text("From \(source.label)").font(.caption).foregroundStyle(.secondary)
            TextField(example, text: $query, axis: .vertical)
                .monoFont()
                .lineLimit(1...4)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)
                .accessibilityLabel(Text("Query"))
            actions
            rangePicker
            if !saved.isEmpty { savedQueries }
            resultList
        }
        .padding(.horizontal)
        .padding(.top, 8)
    }

    private var actions: some View {
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        let isSaved = saved.contains(trimmed)
        return HStack {
            Button("Search") { searches += 1 }
                .buttonStyle(.borderedProminent)
                .disabled(trimmed.isEmpty)
            Button("Labels") { labelsOpen = true }
                .buttonStyle(.bordered)
            Button {
                toggleSaved(trimmed)
            } label: {
                Image(systemName: isSaved ? "bookmark.fill" : "bookmark")
            }
            .accessibilityLabel(isSaved ? Text("Forget this query") : Text("Save this query"))
            .disabled(trimmed.isEmpty)
        }
    }

    private var rangePicker: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack {
                ForEach(logSearchHours, id: \.self) { h in
                    rangeButton(selected: hours == h, title: rangeTitle(h)) { setHours(h) }
                }
                rangeButton(selected: !logSearchHours.contains(hours), title: Text("Custom")) {
                    customHours = String(hours)
                    customOpen = true
                }
            }
        }
    }

    private func rangeTitle(_ h: Int) -> Text {
        h % 24 == 0 && h > 24 ? Text("\(h / 24) d") : Text("\(h) h")
    }

    @ViewBuilder
    private func rangeButton(selected: Bool, title: Text, action: @escaping () -> Void) -> some View {
        if selected {
            Button(action: action) { title }.buttonStyle(.borderedProminent)
        } else {
            Button(action: action) { title }.buttonStyle(.bordered)
        }
    }

    private var savedQueries: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack {
                ForEach(saved, id: \.self) { item in
                    Button {
                        query = item
                        searches += 1
                    } label: {
                        Text(verbatim: item).monoFont().lineLimit(1).frame(maxWidth: 220)
                    }
                    .buttonStyle(.bordered)
                }
            }
        }
    }

    @ViewBuilder private var resultList: some View {
        if let result {
            LoadStateView(state: result, retry: { searches += 1 }) { logs in
                LogSearchLines(logs: logs)
            }
        } else {
            ContentUnavailableView("Type a query or pick a label", systemImage: "text.magnifyingglass")
        }
    }

    private var noSource: some View {
        VStack(spacing: 12) {
            if discovering {
                ProgressView("Looking for a log backend…")
            } else {
                ContentUnavailableView {
                    Label("No log backend found", systemImage: "text.magnifyingglass")
                } description: {
                    if let discoveryError {
                        Text(verbatim: discoveryError)
                    } else {
                        Text("Set the URL and token of your Loki or VictoriaLogs, or install one in the cluster.")
                    }
                } actions: {
                    Button("Set up a source") { sourceOpen = true }.buttonStyle(.borderedProminent)
                    Button("Search again") { Task { await discover(autoSelect: true) } }
                }
            }
        }
    }

    // MARK: - Loading

    private func resolve() async {
        let config = LogsBackendStore.read(fingerprint)
        saved = config.saved
        source = config.source
        result = nil
        query = initialQuery
        if source == nil {
            discovering = true
            await discover(autoSelect: true)
        }
        resolved = true
        searchFirst()
    }

    /// Runs the query the screen opened on, or the demo's sample.
    private func searchFirst() {
        guard let source else { return }
        if model.activeSummary?.demo == true, query.isEmpty, source.kind == logsKindLoki {
            query = "{namespace=\"demo-shop\"}"
        }
        if !query.isEmpty { searches += 1 }
    }

    private func discover(autoSelect: Bool) async {
        guard let client = model.client else {
            discovering = false
            return
        }
        discovering = true
        discoveryError = nil
        do {
            let found = try await client.logsDiscover()
            discovered = found
            if autoSelect, source == nil, let first = found.first {
                source = first
                if resolved { searchFirst() }
            }
        } catch {
            discoveryError = error.localizedDescription
        }
        discovering = false
    }

    private func setSource(_ next: PromSource) async -> String? {
        guard let client = model.client else { return nil }
        do {
            let checked = try await client.logsProbe(next)
            var config = LogsBackendStore.read(fingerprint)
            config.source = checked
            try LogsBackendStore.save(config, for: fingerprint)
            source = checked
            result = nil
            searchFirst()
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func test(_ candidate: PromSource) async -> String? {
        guard let client = model.client else { return nil }
        do {
            _ = try await client.logsProbe(candidate)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func setHours(_ next: Int) {
        hours = next
        if !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { searches += 1 }
    }

    private func toggleSaved(_ trimmed: String) {
        let next = saved.contains(trimmed) ? saved.filter { $0 != trimmed } : logSearchSaved(saved, adding: trimmed)
        saved = next
        var config = LogsBackendStore.read(fingerprint)
        config.saved = next
        try? LogsBackendStore.save(config, for: fingerprint)
    }

    private func search() async {
        guard searches > 0, let client = model.client, let source else { return }
        let asked = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !asked.isEmpty else { return }
        generation += 1
        let mine = generation
        let current = { !Task.isCancelled && mine == generation }
        result = .loading
        do {
            let logs = try await client.logsQuery(source, query: asked, hours: hours)
            guard current() else { return }
            result = .loaded(logs, at: Date())
        } catch {
            guard current() else { return }
            result = .failed(error.localizedDescription)
        }
    }
}

/// A search's lines, newest first, and how many came back.
private struct LogSearchLines: View {
    let logs: LogsResult

    var body: some View {
        if logs.lines.isEmpty {
            ContentUnavailableView("No log yet", systemImage: "doc.text")
        } else {
            // A List, as PodLogsSheet's: a lazy stack bounces over wrapped lines.
            List(logs.lines.indices, id: \.self) { index in
                LogSearchLine(line: logs.lines[index])
                    .listRowInsets(EdgeInsets(top: 2, leading: 0, bottom: 2, trailing: 0))
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
            }
            .listStyle(.plain)
            .scalableMonoText(.logs)
            .scrollContentBackground(.hidden)
            .safeAreaInset(edge: .bottom) { footer }
        }
    }

    private var footer: some View {
        HStack(spacing: 4) {
            Text("\(logs.lines.count) lines")
            if logs.stats?.truncated == true {
                Text(verbatim: "·")
                Text("truncated")
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .monospacedDigit()
        .frame(maxWidth: .infinity)
        .padding(.vertical, 6)
        .background(.bar)
    }
}

private struct LogSearchLine: View {
    let line: LogsLine

    var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(spacing: 6) {
                Text(
                    Date(timeIntervalSince1970: Double(line.time) / 1000),
                    format: .dateTime.day().month().hour().minute().second()
                )
                .monospacedDigit()
                if let pod = line.pod { Text(verbatim: pod).lineLimit(1) }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            Text(verbatim: line.text)
                .monoFont()
                .frame(maxWidth: .infinity, alignment: .leading)
                .textSelection(.enabled)
        }
    }
}

/// "Open in Loki" or "Open in VictoriaLogs", by the backend's kind.
func openInLogsBackendTitle(_ source: PromSource) -> String {
    source.kind == logsKindVictoriaLogs ? String(localized: "Open in VictoriaLogs") : String(localized: "Open in Loki")
}
