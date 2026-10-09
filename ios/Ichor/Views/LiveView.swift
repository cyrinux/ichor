import Charts
import SwiftUI
import IchorCore

/// Validated chart colors (dataviz reference palette slots 1-3, light and dark steps).
enum ChartPalette {
    static let first = Color(UIColor { $0.userInterfaceStyle == .dark ? UIColor(rgb: 0x3987E5) : UIColor(rgb: 0x2A78D6) })
    static let second = Color(UIColor { $0.userInterfaceStyle == .dark ? UIColor(rgb: 0xD95926) : UIColor(rgb: 0xEB6834) })
    static let third = Color(UIColor { $0.userInterfaceStyle == .dark ? UIColor(rgb: 0x199E70) : UIColor(rgb: 0x1BAF7A) })
}

/// Polled history; owned by the node screen so it survives tab switches.
@Observable
@MainActor
final class LiveStats {
    static let pollSeconds: Double = 2
    static let maxPoints = 90 // 3 minutes

    private(set) var points: [StatsPoint] = []
    private(set) var cpuCount = 0
    private(set) var detail: Bottlenecks?
    private(set) var error: String?
    private var last: NodeStats?

    func poll(_ client: TalosClient, node: String) async {
        while !Task.isCancelled {
            do {
                let sample = try await client.stats(node: node)
                try Task.checkCancellation()
                if let last {
                    detail = try await TalosClient.bottlenecks(previous: last, current: sample)
                    if let point = ratesBetween(last, sample) {
                        points = Array((points + [point]).suffix(Self.maxPoints))
                    }
                }
                try Task.checkCancellation()
                last = sample
                cpuCount = sample.cpuCount
                error = nil
            } catch {
                last = nil
                detail = nil
                self.error = error.localizedDescription
            }
            try? await Task.sleep(for: .seconds(Self.pollSeconds))
        }
    }
}

struct LiveView: View {
    let node: String
    let stats: LiveStats
    @Environment(AppModel.self) private var model

    var body: some View {
        List {
            if let error = stats.error { ErrorOrNoticeText(message: error) }
            // An error says why nothing comes: "collecting" would claim otherwise.
            if stats.points.isEmpty && stats.error == nil { Text("Collecting samples every 2s…").foregroundStyle(.secondary) }
            LiveChart(title: cpuTitle,
                      points: stats.points, series: [(String(localized: "CPU"), ChartPalette.first, \.cpuPercent)],
                      format: { String(format: "%.0f%%", $0) }, fixedMax: 100)
            LiveChart(title: memoryTitle,
                      points: stats.points, series: [(String(localized: "Memory"), ChartPalette.first, \.memPercent)],
                      format: { String(format: "%.0f%%", $0) }, fixedMax: 100)
            LiveChart(title: String(localized: "Network"), points: stats.points,
                      series: [(String(localized: "in"), ChartPalette.first, \.rxPerSec),
                               (String(localized: "out"), ChartPalette.second, \.txPerSec)],
                      format: { formatBytes(UInt64(max($0, 0))) + "/s" })
            LiveChart(title: String(localized: "Disk"), points: stats.points,
                      series: [(String(localized: "read"), ChartPalette.first, \.readPerSec),
                               (String(localized: "write"), ChartPalette.second, \.writePerSec)],
                      format: { formatBytes(UInt64(max($0, 0))) + "/s" })
            if let detail = stats.detail { BottleneckSection(detail: detail) }
            LiveChart(title: String(localized: "Load (1 min)"), points: stats.points,
                      series: [(String(localized: "Load"), ChartPalette.first, \.load1)],
                      format: { String(format: "%.2f", $0) })
        }
        .themedBackground()
        // Polls only while visible: the task is cancelled when the tab or screen goes away.
        .task {
            guard let client = model.client else { return }
            await stats.poll(client, node: node)
        }
    }

    private var cpuTitle: String {
        let cpu = String(localized: "CPU")
        guard stats.cpuCount > 0 else { return cpu }
        let threads = String(localized: "\(stats.cpuCount) threads")
        return "\(cpu) (\(threads))"
    }

    private var memoryTitle: String {
        let memory = String(localized: "Memory")
        guard let last = stats.points.last else { return memory }
        let used = String(localized: "\(formatBytes(last.memUsed)) used")
        return "\(memory) (\(used))"
    }
}

/// One-axis line chart, 2pt lines, headline value(s) in text ink, drag to scrub.
private struct LiveChart: View {
    let title: String
    let points: [StatsPoint]
    let series: [(label: String, color: Color, value: KeyPath<StatsPoint, Double>)]
    let format: (Double) -> String
    var fixedMax: Double?

    @State private var selection: Date?

    private var shown: StatsPoint? {
        guard let selection else { return points.last }
        return points.min { abs($0.at.timeIntervalSince(selection)) < abs($1.at.timeIntervalSince(selection)) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(title).font(.subheadline.weight(.semibold))
                Spacer()
                if selection != nil, let shown, let last = points.last {
                    Text("\(Int(last.at.timeIntervalSince(shown.at)))s ago").font(.caption2).foregroundStyle(.secondary)
                }
            }
            HStack(spacing: 16) {
                ForEach(series, id: \.label) { s in
                    HStack(spacing: 6) {
                        if series.count > 1 {
                            Circle().fill(s.color).frame(width: 8, height: 8)
                            Text(s.label).font(.caption).foregroundStyle(.secondary)
                        }
                        Text(shown.map { format($0[keyPath: s.value]) } ?? "—")
                            .font(series.count > 1 ? .headline : .title2.weight(.semibold))
                            .monospacedDigit()
                    }
                }
            }
            Chart {
                ForEach(series, id: \.label) { s in
                    ForEach(points) { p in
                        LineMark(x: .value("Time", p.at), y: .value(title, p[keyPath: s.value]), series: .value("Series", s.label))
                            .foregroundStyle(s.color)
                            .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                    }
                }
                if selection != nil, let shown {
                    RuleMark(x: .value("Selected", shown.at)).foregroundStyle(.secondary.opacity(0.5))
                }
            }
            .chartYScale(domain: 0...(fixedMax ?? max((points.map { p in series.map { p[keyPath: $0.value] }.max() ?? 0 }.max() ?? 0) * 1.15, 0.001)))
            .chartXAxis(.hidden)
            .chartYAxis { AxisMarks(position: .leading, values: .automatic(desiredCount: 3)) { AxisGridLine() } }
            .chartXSelection(value: $selection)
            .frame(height: 120)
            .accessibilityLabel("\(title) chart")
        }
        .padding(.vertical, 4)
    }
}
