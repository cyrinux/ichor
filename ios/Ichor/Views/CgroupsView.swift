import Charts
import SwiftUI
import IchorCore

/// Polled cgroup tree (`talosctl cgroups`); owned by the node screen like ProcessMonitor.
@Observable
@MainActor
final class CgroupMonitor {
    /// A copy of /sys/fs/cgroup (several MB on a busy node) is heavy on mobile data: poll slowly.
    static let pollSeconds: Double = 10
    /// Pressure samples the chart keeps: 5 minutes at one per poll.
    static let historyPoints = 30

    private(set) var previous: CgroupReport?
    private(set) var current: CgroupReport?
    private(set) var error: String?
    /// The node's pressure at each poll, oldest first, for the chart.
    private(set) var history: [PressureSample] = []

    func poll(_ client: TalosClient, node: String) async {
        while !Task.isCancelled {
            do {
                let next = try await client.cgroups(node: node)
                previous = current
                current = next
                history = Array((history + [PressureSample(at: next.at, pressure: next.pressure)]).suffix(Self.historyPoints))
                error = nil
            } catch {
                if !Task.isCancelled { self.error = error.localizedDescription }
            }
            try? await Task.sleep(for: .seconds(Self.pollSeconds))
        }
    }
}

/// The cgroup tree: Talos services, pods and containers with memory, CPU and what they wait for.
struct CgroupsView: View {
    let node: String
    let monitor: CgroupMonitor

    @Environment(AppModel.self) private var model
    @State private var sort = CgroupSort.memory
    @State private var expanded: Set<String>?

    var body: some View {
        List {
            Section {
                if let error = monitor.error {
                    ErrorOrNoticeText(message: error)
                }
                if monitor.current == nil && monitor.error == nil {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Loading cgroups…").foregroundStyle(.secondary)
                    }
                }
                Picker("Sort by", selection: $sort) {
                    Text("Memory").tag(CgroupSort.memory)
                    Text("CPU").tag(CgroupSort.cpu)
                    Text("Pressure").tag(CgroupSort.pressure)
                }
                .pickerStyle(.segmented)
            } footer: {
                Text("CPU as a share of one core and disk I/O since the last refresh, every 10 s; pressure over the last 10 s. Tap a group to open it.")
            }
            if monitor.history.count >= 2 {
                Section { PressureChart(samples: monitor.history) }
            }
            if let current = monitor.current {
                let open = expanded ?? defaultExpandedCgroups(current)
                Section {
                    ForEach(cgroupRows(previous: monitor.previous, current: current, expanded: open, sort: sort)) { row in
                        CgroupRowView(row: row, expanded: open.contains(row.id))
                            .contentShape(Rectangle())
                            .onTapGesture {
                                guard row.hasChildren else { return }
                                expanded = open.symmetricDifference([row.id])
                            }
                    }
                }
            }
        }
        .themedBackground()
        // Polls only while visible: the task is cancelled when the tab or screen goes away.
        .task {
            guard let client = model.client else { return }
            await monitor.poll(client, node: node)
        }
    }
}

private struct CgroupRowView: View {
    let row: CgroupRow
    let expanded: Bool

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: expanded ? "chevron.down" : "chevron.right")
                .font(.caption)
                .foregroundStyle(.secondary)
                .opacity(row.hasChildren ? 1 : 0)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline) {
                    Text(verbatim: row.node.name)
                        .font(row.node.kind == "service" || row.node.kind == "pod" ? Font.subheadline.weight(.semibold) : Font.subheadline)
                        .lineLimit(1)
                    Spacer()
                    Text(verbatim: row.cpuPercent.map { String(format: "%.1f%%", $0) } ?? "—")
                        .font(.caption.monospacedDigit())
                    Text(verbatim: formatBytes(row.node.memCurrent))
                        .font(.caption.monospacedDigit())
                }
                notes
            }
        }
        .padding(.leading, CGFloat(row.depth) * 14)
    }

    /// Only what stands out: a memory limit, disk traffic, OOM kills and pressure worth a look.
    @ViewBuilder private var notes: some View {
        let n = row.node
        let waiting = n.pressure.map { p in
            [(String(localized: "CPU"), p.cpu.some10), (String(localized: "Memory"), p.memory.some10), (String(localized: "Disk I/O"), p.io.some10)]
                .filter { pressureLevel($0.1) != .ok }
        } ?? []
        let hasNotes = n.memMax > 0 || (row.ioPerSecond ?? 0) >= 1024 || n.oomKills > 0
        if hasNotes || !waiting.isEmpty {
            HStack(spacing: 8) {
                if n.memMax > 0 { Text("limit \(formatBytes(n.memMax))") }
                if let io = row.ioPerSecond, io >= 1024 { Text("disk \(formatBytes(UInt64(io)))/s") }
                if n.oomKills > 0 { Text("OOM-killed \(Int(n.oomKills)) times").foregroundStyle(.statusBad) }
                ForEach(waiting, id: \.0) { label, value in
                    Text("waits for \(label) \(String(format: "%.1f%%", value))").foregroundStyle(pressureColor(value))
                }
            }
            .font(.caption2)
            .foregroundStyle(.secondary)
            .lineLimit(1)
        }
    }
}

/// One poll's pressure, for the chart.
struct PressureSample: Identifiable {
    /// Unix ms.
    let at: Int64
    let pressure: CgroupPressure

    var id: Int64 { at }
    var date: Date { Date(epochMillis: at) }
}

/// The node's PSI "some" 10 s averages at each poll while the tab is open: how the waiting for
/// CPU, memory and disk moves. The scale reaches at least 10 % so a quiet node reads as flat.
private struct PressureChart: View {
    let samples: [PressureSample]

    @State private var selection: Date?

    private var series: [(label: String, color: Color, value: KeyPath<CgroupPressure, CgroupPSI>)] {
        [(String(localized: "CPU"), ChartPalette.first, \.cpu),
         (String(localized: "Memory"), ChartPalette.second, \.memory),
         (String(localized: "Disk I/O"), ChartPalette.third, \.io)]
    }

    private var shown: PressureSample? {
        guard let selection else { return samples.last }
        return samples.min { abs($0.date.timeIntervalSince(selection)) < abs($1.date.timeIntervalSince(selection)) }
    }

    var body: some View {
        let peak = samples.flatMap { s in series.map { s.pressure[keyPath: $0.value].some10 } }.max() ?? 0
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Pressure").font(.subheadline.weight(.semibold))
                Spacer()
                if selection != nil, let shown, let last = samples.last {
                    Text("\(Int(last.date.timeIntervalSince(shown.date)))s ago").font(.caption2).foregroundStyle(.secondary)
                }
            }
            HStack(spacing: 14) {
                ForEach(series, id: \.label) { s in
                    HStack(spacing: 6) {
                        Circle().fill(s.color).frame(width: 8, height: 8)
                        Text(verbatim: s.label).font(.caption).foregroundStyle(.secondary)
                        Text(verbatim: shown.map { String(format: "%.1f%%", $0.pressure[keyPath: s.value].some10) } ?? "—")
                            .font(.subheadline.weight(.semibold))
                            .monospacedDigit()
                    }
                }
            }
            Chart {
                ForEach(series, id: \.label) { s in
                    ForEach(samples) { p in
                        LineMark(x: .value("Time", p.date), y: .value("Pressure", p.pressure[keyPath: s.value].some10), series: .value("Series", s.label))
                            .foregroundStyle(s.color)
                            .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                    }
                }
                if selection != nil, let shown {
                    RuleMark(x: .value("Selected", shown.date)).foregroundStyle(.secondary.opacity(0.5))
                }
            }
            .chartYScale(domain: 0...Swift.min(Swift.max(peak * 1.15, 10), 100))
            .chartXAxis(.hidden)
            .chartYAxis { AxisMarks(position: .leading, values: .automatic(desiredCount: 3)) { AxisGridLine() } }
            .chartXSelection(value: $selection)
            .frame(height: 120)
            .accessibilityLabel(Text("\(String(localized: "Pressure")) chart"))
        }
        .padding(.vertical, 4)
    }
}

/// The node's pressure in Resources: how much of the last 10 s tasks waited for CPU, memory
/// and disk, who waited the most, and workloads OOM-killed or near their memory limit.
struct PressureSection: View {
    let state: LoadState<CgroupReport>
    let onDetails: () -> Void

    var body: some View {
        Section {
            switch state {
            case .loading:
                ProgressView()
            case .failed(let message):
                Text("Pressure unavailable: \(message)").font(.caption).foregroundStyle(.statusBad)
            case .loaded(let report, _, _):
                pressureRow("CPU", report.pressure.cpu, report.mostAffected("cpu"))
                pressureRow("Memory", report.pressure.memory, report.mostAffected("memory"))
                pressureRow("Disk I/O", report.pressure.io, report.mostAffected("io"))
                ForEach(report.alerts, id: \.self) { alert in
                    Label(alertText(alert), systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(alert.kind == "oomKill" ? .red : .orange)
                }
            }
            Button("Details", action: onDetails)
        } header: {
            Text("Pressure")
        } footer: {
            Text("Share of the last 10 s that tasks waited for each resource")
        }
    }

    private func pressureRow(_ label: LocalizedStringKey, _ psi: CgroupPSI, _ most: String?) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            LabeledContent {
                Text(verbatim: String(format: "%.1f%%", psi.some10))
                    .monospacedDigit()
                    .foregroundStyle(pressureColor(psi.some10))
            } label: {
                Text(label)
            }
            // Who waits the most is only worth naming once the node itself waits.
            if let most, pressureLevel(psi.some10) != .ok {
                Text("Most affected: \(most)").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private func alertText(_ alert: CgroupAlert) -> String {
        alert.kind == "oomKill"
            ? String(localized: "\(alert.who): OOM-killed \(alert.count) times since boot")
            : String(localized: "\(alert.who): memory at \(Int(alert.percent))% of its limit")
    }
}

func pressureColor(_ some10: Double) -> Color {
    switch pressureLevel(some10) {
    case .ok: .primary
    case .warn: .orange
    case .bad: .red
    }
}
