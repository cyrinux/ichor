import SwiftUI
import IchorCore

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

/// Pods running on the node, grouped from CRI containers, with Talos' own containers first: CPU%
/// from CPU time deltas between two polls, memory as reported by the runtime. A long press on a
/// container restarts it (os:admin): typed hostname for a system container, a plain confirm for
/// a Kubernetes one (the kubelet starts it again).
struct PodsView: View {
    let node: String
    let hostname: String
    let monitor: PodMonitor
    /// What the node's Talos version says about restarting a container.
    var restartSupport = FeatureSupport(supported: true)

    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var sort = ProcessSort.cpu
    /// A system container waiting for the typed hostname.
    @State private var systemRestart: NodeContainer?
    /// A Kubernetes container waiting for a plain confirm.
    @State private var kubeRestart: NodeContainer?
    @State private var restarting = false
    @State private var resultMessage: String?

    var body: some View {
        let shown = sortPods(filterPods(monitor.pods, query: query), by: sort)
        List {
            Section {
                if let error = monitor.error { ErrorOrNoticeText(message: error) }
                if let sample = monitor.sample {
                    LabeledContent("Pods", value: "\(monitor.pods.filter { !$0.isSystem }.count)")
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
                Text("Refreshed every 3s. CPU is a percentage of one core.") + Text(verbatim: " ") + Text("Tap a container to read its log.")
            }
            ForEach(shown) { pod in
                Section {
                    // A container row opens its log.
                    ForEach(pod.containers) { row in
                        NavigationLink(value: Route.containerLogs(node: node, hostname: hostname, container: logContainer(row.container))) {
                            ContainerRowView(row: row)
                        }
                        .contextMenu {
                            if model.allows(.containerRestart) && !restarting {
                                FeatureButton(title: String(localized: "Restart container…"), systemImage: "arrow.clockwise",
                                              support: restartSupport, role: .destructive) {
                                    if row.container.isSystem { systemRestart = row.container } else { kubeRestart = row.container }
                                }
                            }
                        }
                    }
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
        .sheet(item: $systemRestart) { container in
            HostnameConfirmationSheet(
                title: confirmationTitle(container),
                message: String(localized: "This is a Talos system container: what it serves stops until it is back."),
                hostname: hostname,
                actionTitle: String(localized: "Restart")
            ) {
                systemRestart = nil
                Task { await restart(container) }
            }
        }
        .confirmationDialog(kubeRestart.map(confirmationTitle) ?? "", isPresented: $kubeRestart.isPresent(),
                            titleVisibility: .visible, presenting: kubeRestart) { container in
            Button("Restart", role: .destructive) { Task { await restart(container) } }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("The kubelet starts the container again; the pod keeps running.")
        }
        .alert(resultMessage ?? "", isPresented: $resultMessage.isPresent()) {
            Button("OK") {}
        }
        .themedBackground()
        // Polls only while visible: the task is cancelled when the tab or screen goes away.
        .task {
            guard let client = model.client else { return }
            await monitor.poll(client, node: node)
        }
    }
}

extension PodsView {
    private func confirmationTitle(_ container: NodeContainer) -> String {
        String(localized: "Restart container \(container.displayName) on \(hostname)?")
    }

    /// Asks for Face ID / passcode when the app lock is on, then restarts the container; the
    /// next poll shows it back (with a new id for a Kubernetes container).
    private func restart(_ container: NodeContainer) async {
        kubeRestart = nil
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: confirmationTitle(container)) {
            resultMessage = failure
            return
        }
        restarting = true
        do {
            try await client.containerRestart(node: node, namespace: container.namespace, id: container.id)
            resultMessage = String(localized: "Restart of container \(container.displayName) requested on \(hostname).")
        } catch {
            resultMessage = String(localized: "Restarting container \(container.displayName) failed: \(error.localizedDescription)")
        }
        restarting = false
    }
}

/// "pod/container" for the log screen's title.
private func logContainer(_ container: NodeContainer) -> LogContainer {
    let name = container.name.isEmpty ? container.id : container.name
    return LogContainer(id: container.id, name: container.pod.isEmpty ? name : "\(container.pod)/\(name)")
}

private struct PodHeader: View {
    let pod: PodGroup

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 1) {
                Group {
                    if pod.isSystem { Text("System") } else { Text(verbatim: pod.pod) }
                }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(pod.allRunning ? Color.primary : Color.orange)
                .textCase(nil)
                .lineLimit(1)
                if pod.isSystem {
                    Text("Talos containers").font(.caption2).textCase(nil)
                } else if !pod.namespace.isEmpty {
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
                Text(verbatim: container.displayName)
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
