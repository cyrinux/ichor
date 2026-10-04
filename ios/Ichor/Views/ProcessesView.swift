import SwiftUI
import IchorCore

/// Polled process list (`talosctl processes`); owned by the node screen like LiveStats.
@Observable
@MainActor
final class ProcessMonitor {
    static let pollSeconds: Double = 2

    private(set) var rows: [ProcessRow] = []
    private(set) var sample: ProcessSample?
    private(set) var error: String?

    func poll(_ client: TalosClient, node: String) async {
        while !Task.isCancelled {
            do {
                let next = try await client.processes(node: node)
                rows = processRows(previous: sample, current: next)
                sample = next
                error = nil
            } catch {
                if !Task.isCancelled { self.error = error.localizedDescription }
            }
            try? await Task.sleep(for: .seconds(Self.pollSeconds))
        }
    }
}

/// A `top`-like view: CPU% from CPU time deltas between two polls, memory as RSS.
struct ProcessesView: View {
    let node: String
    let monitor: ProcessMonitor

    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var sort = ProcessSort.cpu
    @State private var expanded: Set<Int32> = []

    var body: some View {
        let shown = sortProcesses(filterProcesses(monitor.rows, query: query), by: sort)
        List {
            Section {
                if let error = monitor.error {
                    ErrorOrNoticeText(message: error)
                }
                if let sample = monitor.sample {
                    LabeledContent("Processes", value: "\(sample.processes.count)")
                    LabeledContent("Memory (RSS)", value: formatBytes(totalRSS(sample.processes)))
                } else if monitor.error == nil {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Loading processes…").foregroundStyle(.secondary)
                    }
                }
                Picker("Sort by", selection: $sort) {
                    Text("CPU").tag(ProcessSort.cpu)
                    Text("Memory").tag(ProcessSort.memory)
                }
                .pickerStyle(.segmented)
            } footer: {
                Text("Refreshed every 2s. CPU is a percentage of one core; tap a process to see its full command line.")
            }
            Section {
                ForEach(shown) { row in
                    Button { expanded = expanded.symmetricDifference([row.id]) } label: {
                        ProcessRowView(row: row, expanded: expanded.contains(row.id))
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityValue(expanded.contains(row.id) ? Text("Expanded") : Text("Collapsed"))
                }
            }
        }
        .overlay {
            if shown.isEmpty && !query.isEmpty && monitor.sample != nil {
                ContentUnavailableView.search(text: query)
            }
        }
        .searchable(text: $query, prompt: Text("Command or arguments"))
        .themedBackground()
        // Polls only while visible: the task is cancelled when the tab or screen goes away.
        .task {
            guard let client = model.client else { return }
            await monitor.poll(client, node: node)
        }
    }
}

private struct ProcessRowView: View {
    let row: ProcessRow
    let expanded: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: row.process.command.isEmpty ? "—" : row.process.command)
                    .font(.headline)
                    .lineLimit(1)
                Spacer()
                Text(verbatim: cpu)
                    .font(.subheadline.weight(.semibold))
                    .monospacedDigit()
            }
            if !row.process.args.isEmpty {
                Text(verbatim: row.process.args)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(expanded ? nil : 1)
            }
            HStack(spacing: 12) {
                Text(verbatim: formatBytes(row.process.rss))
                Text("\(Int(row.process.threads)) threads")
                Text(verbatim: "PID \(row.process.pid)")
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .monospacedDigit()
        }
    }

    private var cpu: String {
        row.cpuPercent.map { String(format: "%.1f%%", $0) } ?? "—"
    }
}
