import SwiftUI
import Charts
import IchorCore

/// A panel's last answer: `result` stays shown while it reloads or when a reload fails.
struct PanelResult: Equatable {
    var result: PromResult?
    var error: String?
    var loading = false
}

extension MetricsRange {
    var label: String {
        switch self {
        case .minutes15: String(localized: "15m")
        case .hour1: String(localized: "1h")
        case .hours6: String(localized: "6h")
        case .day1: String(localized: "1d")
        case .days7: String(localized: "7d")
        }
    }
}

/// PromQL panels of the cluster on screen, from its Prometheus, Mimir, Thanos or
/// VictoriaMetrics: found in the cluster (service proxy) or at a URL set by the user. On first
/// open it looks for a query API and, when it finds one, starts with the built-in panels.
struct MetricsView: View {
    @Environment(AppModel.self) private var model
    @State private var config = MetricsConfig()
    @State private var loaded = false
    @State private var presets: [PromPanel] = []
    @State private var discovered: [PromSource]?
    @State private var discovering = false
    @State private var discoveryError: String?
    @State private var range = MetricsRange.hour1
    @State private var results: [String: PanelResult] = [:]
    @State private var editing: PromPanel?
    @State private var sourceOpen = false
    @State private var saveError: String?

    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }

    /// What the panels show: a change reloads them (and restarts the minute refresh).
    private struct Shown: Equatable { let loaded: Bool; let source: PromSource?; let panels: [PromPanel]; let range: MetricsRange }

    var body: some View {
        Group {
            if !loaded {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let source = config.source {
                panels(source)
            } else {
                noSource
            }
        }
        .navigationTitle("Metrics")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if config.source != nil {
                    Button { editing = PromPanel() } label: { Image(systemName: "plus") }
                        .accessibilityLabel(Text("Add panel"))
                }
                Button { sourceOpen = true } label: { Image(systemName: "slider.horizontal.3") }
                    .accessibilityLabel(Text("Metrics source"))
            }
        }
        .task(id: fingerprint) { await load() }
        .task(id: Shown(loaded: loaded, source: config.source, panels: config.panels, range: range)) {
            guard loaded else { return }
            while !Task.isCancelled {
                await refresh()
                try? await Task.sleep(for: .seconds(60))
            }
        }
        .sheet(item: $editing) { panel in
            PanelEditorSheet(initial: panel, presets: presets, preview: preview) { saved in
                let stored = saved.id.isEmpty ? PromPanel(id: UUID().uuidString, title: saved.title, query: saved.query, unit: saved.unit, legend: saved.legend) : saved
                save(config.saving(stored))
            }
        }
        .sheet(isPresented: $sourceOpen) {
            SourceSheet(current: config.source, discovered: discovered, discovering: discovering, discoveryError: discoveryError,
                        discover: { await discover(autoSelect: false) }, test: test, save: setSource)
        }
        .themedBackground()
    }

    private func panels(_ source: PromSource) -> some View {
        List {
            Section {
                Text("Source: \(source.label)").font(.footnote).foregroundStyle(.secondary)
                Picker("Range", selection: $range) {
                    ForEach(MetricsRange.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                .pickerStyle(.segmented)
                if let saveError { Text(saveError).foregroundStyle(.red).font(.footnote) }
                if config.panels.isEmpty { Text("No panels yet: add one from a preset or write PromQL.").foregroundStyle(.secondary) }
            }
            ForEach(Array(config.panels.enumerated()), id: \.element.id) { index, panel in
                Section {
                    PanelCard(panel: panel, result: results[panel.id] ?? PanelResult(loading: true))
                } header: {
                    HStack {
                        Text(panel.title)
                        Spacer()
                        Menu {
                            Button("Edit") { editing = panel }
                            if index > 0 { Button("Move up") { save(config.moving(panel.id, by: -1)) } }
                            if index < config.panels.count - 1 { Button("Move down") { save(config.moving(panel.id, by: 1)) } }
                            Button("Delete", role: .destructive) {
                                var next = config
                                next.panels.removeAll { $0.id == panel.id }
                                results[panel.id] = nil
                                save(next)
                            }
                        } label: {
                            Image(systemName: "ellipsis.circle")
                        }
                        .accessibilityLabel(Text("More"))
                    }
                }
            }
        }
        .refreshable { await refresh() }
    }

    private var noSource: some View {
        Group {
            if discovering {
                ProgressView("Looking for Prometheus in the cluster…").frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ContentUnavailableView {
                    Label("No Prometheus, Mimir, Thanos or VictoriaMetrics found in the cluster", systemImage: "chart.xyaxis.line")
                } description: {
                    Text(discoveryError ?? String(localized: "Pick a Service by hand, or set the URL of a server the phone can reach."))
                } actions: {
                    Button("Set up a source") { sourceOpen = true }
                    Button("Search the cluster again") { Task { await discover(autoSelect: true) } }
                }
            }
        }
    }

    // MARK: - Loading

    private func load() async {
        config = MetricsStore.read(fingerprint)
        presets = (try? await TalosClient.promPresets()) ?? []
        results = [:]
        loaded = true
        if config.source == nil { await discover(autoSelect: true) }
    }

    private func discover(autoSelect: Bool) async {
        guard let client = model.client else { return }
        discovering = true
        discoveryError = nil
        do {
            let found = try await client.promDiscover()
            discovered = found
            if autoSelect, config.source == nil, let first = found.first { _ = await setSource(first) }
        } catch {
            discoveryError = error.localizedDescription
        }
        discovering = false
    }

    /// Checks `source` (Go), saves it; the first source brings the built-in panels. Nil when saved, else why not.
    private func setSource(_ source: PromSource) async -> String? {
        do {
            let checked = try await TalosClient.normalizePromSource(source)
            var next = config
            next.source = checked
            if next.panels.isEmpty { next.panels = presets.map { PromPanel(id: UUID().uuidString, title: $0.title, query: $0.query, unit: $0.unit, legend: $0.legend) } }
            results = [:]
            save(next)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func test(_ source: PromSource) async -> String? {
        guard let client = model.client else { return nil }
        do {
            let checked = try await TalosClient.normalizePromSource(source)
            let now = Int64(Date().timeIntervalSince1970)
            _ = try await client.promRange(checked, query: "vector(1)", start: now - 60, end: now)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func preview(_ panel: PromPanel) async -> Result<PromResult, Error> {
        guard let client = model.client, let source = config.source else {
            return .failure(TalosError(message: String(localized: "No Prometheus, Mimir, Thanos or VictoriaMetrics found in the cluster")))
        }
        return await Self.run(client, source, panel.query, range)
    }

    private func save(_ next: MetricsConfig) {
        config = next
        do {
            try MetricsStore.save(next, for: fingerprint)
            saveError = nil
        } catch {
            saveError = error.localizedDescription
        }
    }

    /// Every panel at once (Go calls run on their own threads); answers for a panel since
    /// edited or deleted, or for another source or range, are dropped.
    private func refresh() async {
        guard let client = model.client, let source = config.source else { return }
        let range = range
        let panels = config.panels
        for panel in panels { results[panel.id, default: PanelResult()].loading = true }
        await withTaskGroup(of: (PromPanel, Result<PromResult, Error>).self) { group in
            for panel in panels {
                group.addTask { (panel, await Self.run(client, source, panel.query, range)) }
            }
            for await (panel, outcome) in group {
                guard config.source == source, config.panels.contains(panel), self.range == range else { continue }
                switch outcome {
                case .success(let result): results[panel.id] = PanelResult(result: result)
                case .failure(let error):
                    results[panel.id] = PanelResult(result: results[panel.id]?.result, error: error.localizedDescription)
                }
            }
        }
    }

    private static func run(_ client: TalosClient, _ source: PromSource, _ query: String, _ range: MetricsRange) async -> Result<PromResult, Error> {
        let end = Int64(Date().timeIntervalSince1970)
        do {
            return .success(try await client.promRange(source, query: query, start: end - range.seconds, end: end))
        } catch {
            return .failure(error)
        }
    }
}

/// One panel: its chart, or why it has none.
private struct PanelCard: View {
    let panel: PromPanel
    let result: PanelResult

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if result.loading && result.result == nil { ProgressView().frame(maxWidth: .infinity) }
            if let error = result.error { Text(error).font(.footnote).foregroundStyle(.red) }
            if let res = result.result { PromChartView(panel: panel, result: res) }
        }
    }
}

/// A result as a chart with its notes: no data, dropped series, server warnings.
struct PromChartView: View {
    let panel: PromPanel
    let result: PromResult

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if result.series.isEmpty {
                Text("No data for this range.").foregroundStyle(.secondary)
            } else {
                TimeSeriesChart(title: panel.title.isEmpty ? panel.query : panel.title, times: result.times,
                                lines: result.series.map { ($0.legend(panel.legend), $0.values) },
                                format: { formatMetric($0, unit: panel.unit) })
            }
            if result.truncated {
                Text("Series shown: \(result.series.count) of \(result.total)").font(.caption).foregroundStyle(.secondary)
            }
            ForEach(result.warnings, id: \.self) { Text($0).font(.caption).foregroundStyle(.secondary) }
        }
    }
}

/// One y-axis from the lowest value (or 0) to the highest; gaps break the line; drag to
/// scrub. The first three lines take the chart colors in order (color follows the series,
/// not its rank); the others stay muted, never a made-up hue. Every line is listed below
/// with its value at the scrubbed (or latest) time, so identity never rests on color alone.
struct TimeSeriesChart: View {
    let title: String
    let times: [Int64]
    let lines: [(label: String, values: [Double?])]
    let format: (Double) -> String

    @State private var selection: Date?

    private struct Point: Identifiable {
        let id: String
        let line: Int
        let segment: String
        let at: Date
        let value: Double
    }

    private var dates: [Date] { times.map { Date(timeIntervalSince1970: Double($0) / 1000) } }

    private var points: [Point] {
        var out: [Point] = []
        for (li, line) in lines.enumerated() {
            var segment = 0
            for (i, value) in line.values.enumerated() where i < times.count {
                guard let value else { segment += 1; continue }
                out.append(Point(id: "\(li)-\(i)", line: li, segment: "\(li)-\(segment)", at: dates[i], value: value))
            }
        }
        return out
    }

    private func color(_ line: Int) -> Color {
        [ChartPalette.first, ChartPalette.second, ChartPalette.third].indices.contains(line)
            ? [ChartPalette.first, ChartPalette.second, ChartPalette.third][line]
            : Color.secondary.opacity(0.45)
    }

    /// The scrubbed time, else the last one any line has a value at.
    private var shownIndex: Int? {
        if let selection, !dates.isEmpty {
            return dates.indices.min { abs(dates[$0].timeIntervalSince(selection)) < abs(dates[$1].timeIntervalSince(selection)) }
        }
        return times.indices.reversed().first { i in lines.contains { $0.values.indices.contains(i) && $0.values[i] != nil } }
    }

    var body: some View {
        let all = lines.flatMap { $0.values.compactMap { $0 } }
        let top = max(all.max() ?? 0, 0)
        let bottom = min(all.min() ?? 0, 0)
        let upper = bottom + max((top - bottom) * 1.1, 1e-9)
        let shown = shownIndex
        VStack(alignment: .leading, spacing: 6) {
            Chart {
                // Muted lines first, so the colored ones stay on top.
                ForEach(points.reversed()) { p in
                    LineMark(x: .value("Time", p.at), y: .value(title, p.value), series: .value("Series", p.segment))
                        .foregroundStyle(color(p.line))
                        .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                }
                if selection != nil, let shown, dates.indices.contains(shown) {
                    RuleMark(x: .value("Selected", dates[shown])).foregroundStyle(.secondary.opacity(0.5))
                }
            }
            .chartYScale(domain: bottom...upper)
            .chartYAxis { AxisMarks(position: .leading, values: .automatic(desiredCount: 3)) { value in
                AxisGridLine()
                AxisValueLabel { if let v = value.as(Double.self) { Text(format(v)) } }
            } }
            .chartXAxis { AxisMarks(values: .automatic(desiredCount: 3)) { AxisValueLabel(format: .dateTime.hour().minute()) } }
            .chartXSelection(value: $selection)
            .frame(height: 140)
            .accessibilityLabel(Text("Chart of \(title) over time"))
            ForEach(Array(lines.enumerated()), id: \.offset) { li, line in
                HStack(spacing: 8) {
                    if lines.count > 1 { Circle().fill(color(li)).frame(width: 8, height: 8) }
                    Text(verbatim: line.label).font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                    Spacer()
                    Text(verbatim: shown.flatMap { line.values.indices.contains($0) ? line.values[$0] : nil }.map(format) ?? "—")
                        .font(lines.count > 1 ? .callout : .title2.weight(.semibold))
                        .monospacedDigit()
                }
            }
        }
    }
}
