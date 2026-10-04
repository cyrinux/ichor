import Charts
import SwiftUI
import IchorCore

/// Pod-to-pod throughput and p50 latency of the saved tests from `client` to `server`, oldest
/// on the left: one chart per measure (they do not share a scale), with a shared selection so a
/// tap shows the same test in both. Shown once the pair has two tests to compare.
struct NetPerfTrendSection: View {
    let history: [NetPerfReport]
    let client: String
    let server: String

    @State private var selected: Int?

    var body: some View {
        let trend = history.trend(client: client, server: server)
        if trend.count >= 2 {
            let shown = trend[min(selected ?? trend.count - 1, trend.count - 1)]
            Section {
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(verbatim: "\(client) → \(server)").font(.caption.monospaced())
                        Text("pod to pod").font(.caption)
                        Spacer()
                        Text(Date(epochMillis: shown.started), style: .date)
                            .font(.caption2)
                        Text(Date(epochMillis: shown.started), style: .time)
                            .font(.caption2)
                    }
                    .foregroundStyle(.secondary)
                }
                NetPerfTrendChart(label: netPerfTestLabel(NetPerfTest.throughput), values: trend.map(\.throughputMbps),
                                  format: formatMbps, selected: $selected)
                NetPerfTrendChart(label: String(localized: "Latency (p50)"), values: trend.map(\.p50Us),
                                  format: formatMicros, selected: $selected)
            } header: {
                HStack(spacing: 4) {
                    Text("Over time")
                    InfoHint(title: Text("Over time"), text: Text(NetPerfTrendHint.text))
                }
            }
            .onChange(of: "\(client)>\(server)>\(trend.count)") { selected = nil }
        }
    }
}

/// One measure across the tests: evenly spaced points (tests are not evenly spaced in time), a
/// 2 pt line broken where a test has no value, and the selected (or latest) value as the headline.
private struct NetPerfTrendChart: View {
    let label: String
    let values: [Double?]
    let format: (Double) -> String
    @Binding var selected: Int?

    var body: some View {
        let current = min(selected ?? values.count - 1, values.count - 1)
        let shown = values[current]
        let peak = values.compactMap { $0 }.max() ?? 0
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: label).foregroundStyle(.secondary)
                Spacer()
                Text(verbatim: shown.map(format) ?? "—").font(.headline).monospacedDigit()
            }
            Chart {
                // Each run of consecutive values is its own series, so a missing test breaks the line.
                ForEach(Array(values.enumerated()), id: \.offset) { index, value in
                    if let value {
                        LineMark(x: .value("Test", Double(index)), y: .value(label, value), series: .value("Series", run(of: index)))
                            .foregroundStyle(ChartPalette.first)
                            .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                        PointMark(x: .value("Test", Double(index)), y: .value(label, value))
                            .foregroundStyle(ChartPalette.first)
                            .symbolSize(index == (selected ?? values.count - 1) ? 90 : 50)
                    }
                }
                if let selected {
                    RuleMark(x: .value("Selected", Double(selected))).foregroundStyle(.secondary.opacity(0.5))
                }
            }
            .chartXScale(domain: -0.3...(Double(values.count) - 0.7))
            .chartYScale(domain: 0...max(peak * 1.15, 0.001))
            .chartXAxis(.hidden)
            .chartYAxis {
                AxisMarks(position: .leading, values: .automatic(desiredCount: 3)) { value in
                    AxisGridLine()
                    AxisValueLabel { if let v = value.as(Double.self) { Text(verbatim: format(v)) } }
                }
            }
            .chartOverlay { proxy in
                GeometryReader { geometry in
                    // A tap, not a drag, so the list still scrolls over the chart.
                    Rectangle().fill(.clear).contentShape(Rectangle())
                        .onTapGesture { location in
                            guard let plot = proxy.plotFrame else { return }
                            if let index: Double = proxy.value(atX: location.x - geometry[plot].origin.x) {
                                selected = Swift.min(Swift.max(Int(index.rounded()), 0), values.count - 1)
                            }
                        }
                }
            }
            .frame(height: 80)
            // The tap overlay is out of VoiceOver's reach: swipe up or down to pick a test instead.
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("\(label) across the saved tests"))
            .accessibilityValue(Text("Test \(current + 1) of \(values.count): \(shown.map(format) ?? "—")"))
            .accessibilityAdjustableAction { direction in
                switch direction {
                case .increment: selected = Swift.min(current + 1, values.count - 1)
                case .decrement: selected = Swift.max(current - 1, 0)
                @unknown default: break
                }
            }
        }
        .padding(.vertical, 4)
    }

    /// Index of the first test of the unbroken run `index` belongs to.
    private func run(of index: Int) -> Int {
        var start = index
        while start > 0, values[start - 1] != nil { start -= 1 }
        return start
    }
}

/// Where the round trips fell, on a scale from the fastest (left) to the slowest (right): the
/// bar spans p50 to p99, the tick marks p90. A long bar, or one far to the right, is jitter.
struct NetPerfLatencyRange: View {
    let latency: NetPerfLatency

    var body: some View {
        if latency.max > latency.min {
            VStack(spacing: 2) {
                GeometryReader { geometry in
                    let cap: CGFloat = 4
                    let width = geometry.size.width - cap * 2
                    let mid = geometry.size.height / 2
                    let x: (Double) -> CGFloat = { us in cap + CGFloat(Swift.min(Swift.max((us - latency.min) / (latency.max - latency.min), 0), 1)) * width }
                    ZStack(alignment: .topLeading) {
                        Path { p in p.move(to: CGPoint(x: cap, y: mid)); p.addLine(to: CGPoint(x: cap + width, y: mid)) }
                            .stroke(.secondary.opacity(0.35), style: StrokeStyle(lineWidth: 2, lineCap: .round))
                        Path { p in p.move(to: CGPoint(x: x(latency.p50), y: mid)); p.addLine(to: CGPoint(x: x(latency.p99), y: mid)) }
                            .stroke(ChartPalette.first, style: StrokeStyle(lineWidth: 8, lineCap: .round))
                        Path { p in p.move(to: CGPoint(x: x(latency.p90), y: mid - cap)); p.addLine(to: CGPoint(x: x(latency.p90), y: mid + cap)) }
                            .stroke(Color(.secondarySystemGroupedBackground), lineWidth: 2)
                    }
                }
                .frame(height: 14)
                HStack {
                    Text("fastest \(formatMicros(latency.min))")
                    Spacer()
                    Text("slowest \(formatMicros(latency.max))")
                }
                .font(.caption2)
                .foregroundStyle(.secondary)
                .monospacedDigit()
            }
            .padding(.top, 4)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("Round trips from \(formatMicros(latency.min)) to \(formatMicros(latency.max)), p50 \(formatMicros(latency.p50)), p99 \(formatMicros(latency.p99))"))
        }
    }
}

private enum NetPerfTrendHint {
    static let text: LocalizedStringKey = "The saved tests between the client and server chosen above, oldest on the left. Points are evenly spaced, whatever the time between tests.\n\nTap a point to see that test in both charts. A drop in throughput or a jump in latency after a change (CNI, MTU, encryption, a new switch) shows what it cost."
}
