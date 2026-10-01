import SwiftUI
import TalosdevMobileCore

/// Polled Kubernetes containers of a node; owned by the node screen like ProcessMonitor.
@Observable
@MainActor
final class PodMonitor {
    static let pollSeconds: Double = 3

    private(set) var pods: [PodGroup] = []
    private(set) var sample: ContainerSample?
    private(set) var error: String?

    func poll(_ client: TalosClient, node: String) async {
        while !Task.isCancelled {
            do {
                let next = try await client.containers(node: node)
                pods = groupPods(containerRows(previous: sample, current: next))
                sample = next
                error = nil
            } catch {
                if !Task.isCancelled { self.error = error.localizedDescription }
            }
            try? await Task.sleep(for: .seconds(Self.pollSeconds))
        }
    }
}

/// Pods running on the node, grouped from CRI containers: CPU% from CPU time deltas between
/// two polls, memory as reported by the runtime.
struct PodsView: View {
    let node: String
    let monitor: PodMonitor

    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var sort = ProcessSort.cpu

    var body: some View {
        let shown = sortPods(filterPods(monitor.pods, query: query), by: sort)
        List {
            Section {
                if let error = monitor.error {
                    Text(error).font(.footnote).foregroundStyle(.red)
                }
                if let sample = monitor.sample {
                    LabeledContent("Pods", value: "\(monitor.pods.count)")
                    LabeledContent("Containers", value: "\(sample.containers.count)")
                    LabeledContent("Memory", value: formatBytes(monitor.pods.reduce(UInt64(0)) { $0 &+ $1.memory }))
                } else if monitor.error == nil {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Loading pods…").foregroundStyle(.secondary)
                    }
                }
                Picker("Sort by", selection: $sort) {
                    Text("CPU").tag(ProcessSort.cpu)
                    Text("Memory").tag(ProcessSort.memory)
                }
                .pickerStyle(.segmented)
            } footer: {
                Text("Refreshed every 3s. CPU is a percentage of one core.")
            }
            ForEach(shown) { pod in
                Section {
                    ForEach(pod.containers) { ContainerRowView(row: $0) }
                } header: {
                    PodHeader(pod: pod)
                }
            }
        }
        .overlay {
            if shown.isEmpty && monitor.sample != nil {
                if query.isEmpty {
                    ContentUnavailableView("No pods", systemImage: "shippingbox",
                                           description: Text("No Kubernetes containers run on this node."))
                } else {
                    ContentUnavailableView.search(text: query)
                }
            }
        }
        .searchable(text: $query, prompt: Text("Namespace, pod, container or image"))
        .themedBackground()
        // Polls only while visible: the task is cancelled when the tab or screen goes away.
        .task {
            guard let client = model.client else { return }
            await monitor.poll(client, node: node)
        }
    }
}

private struct PodHeader: View {
    let pod: PodGroup

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: pod.pod)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(pod.allRunning ? Color.primary : Color.orange)
                    .textCase(nil)
                    .lineLimit(1)
                if !pod.namespace.isEmpty {
                    Text(verbatim: pod.namespace).font(.caption2).textCase(nil)
                }
            }
            Spacer()
            Text(verbatim: "\(formatCPU(pod.cpuPercent))  ·  \(formatBytes(pod.memory))")
                .font(.caption.weight(.semibold))
                .monospacedDigit()
                .textCase(nil)
        }
    }
}

private struct ContainerRowView: View {
    let row: ContainerRow

    var body: some View {
        let container = row.container
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: container.name.isEmpty ? container.id : container.name)
                    .font(.body)
                    .lineLimit(1)
                Spacer()
                Text(verbatim: formatCPU(row.cpuPercent))
                    .font(.subheadline.weight(.semibold))
                    .monospacedDigit()
            }
            Text(verbatim: container.image)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
            HStack(spacing: 12) {
                Text(verbatim: container.displayStatus)
                    .foregroundStyle(container.isRunning ? Color.secondary : Color.orange)
                Text(verbatim: formatBytes(container.memory))
                if container.pid > 0 { Text(verbatim: "PID \(container.pid)") }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .monospacedDigit()
        }
    }
}

private func formatCPU(_ percent: Double?) -> String {
    percent.map { String(format: "%.1f%%", $0) } ?? "—"
}
