import SwiftUI
import IchorCore

/// A network test between two nodes (netperf, like `cilium connectivity perf`): the setup,
/// then its progress and the measurements. The session belongs to the Kubernetes screen,
/// which stops a running test when it goes away.
struct NetPerfView: View {
    @Bindable var session: NetPerfSession

    @Environment(AppModel.self) private var model
    @State private var nodes: LoadState<[NetPerfNode]> = .loading
    @State private var confirming = false

    var body: some View {
        LoadStateView(state: nodes, retry: load) { nodes in
            Form {
                if let run = session.run {
                    NetPerfStatusSection(run: run, stop: { session.stop() }, reset: { session.reset() })
                    NetPerfResultsSections(setup: run.setup, results: run.results, running: run.running)
                } else {
                    setupSections(nodes.filter(\.ready))
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        .alert(Text("Start a network test?"), isPresented: $confirming) {
            Button("Start") { if let client = model.client { session.start(client: client) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            confirmationMessage(session.setup)
        }
    }

    @ViewBuilder
    private func setupSections(_ ready: [NetPerfNode]) -> some View {
        Section {
            Text("Measures TCP throughput and latency from one node to another with netperf, like cilium connectivity perf. The test runs in a temporary namespace, deleted afterwards.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        if ready.isEmpty {
            Section { Text("No ready node to test.").foregroundStyle(.secondary) }
        } else {
            Section {
                nodePicker("Client node", nodes: ready, selection: $session.setup.client)
                nodePicker("Server node", nodes: ready, selection: $session.setup.server)
            } footer: {
                if session.setup.ready && session.setup.server == session.setup.client {
                    Text("Same node: this measures the node’s own network stack, not a link.")
                        .foregroundStyle(.orange)
                }
            }
            Section {
                Toggle(isOn: $session.setup.hostNetwork) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Also test the host network")
                        Text("Node to node, outside the CNI. The test namespace then needs the privileged pod security level.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            Section("Duration of each measurement") {
                Picker(selection: $session.setup.seconds) {
                    ForEach(netPerfDurations, id: \.self) { seconds in Text("\(seconds) s").tag(seconds) }
                } label: {
                    EmptyView()
                }
                .pickerStyle(.segmented)
            }
            Section {
                Button("Start test") { confirming = true }
                    .disabled(!session.setup.ready)
            }
        }
    }

    private func nodePicker(_ title: LocalizedStringKey, nodes: [NetPerfNode], selection: Binding<String>) -> some View {
        Picker(title, selection: selection) {
            ForEach(nodes) { node in
                Text(verbatim: node.name).font(.callout.monospaced()).tag(node.name)
            }
        }
    }

    private func confirmationMessage(_ setup: NetPerfSetup) -> Text {
        let text = Text("netperf pods run on \(setup.client) and \(setup.server) in a temporary namespace. The link between them is saturated for about \(setup.seconds * setup.steps) seconds, then the namespace is deleted.")
        guard setup.hostNetwork else { return text }
        return text + Text(verbatim: "\n\n") + Text("For the host network, the namespace is created with the privileged pod security level.")
    }

    private func load() async {
        guard let client = model.client else { return }
        nodes = nodes.refreshed(with: await .from { try await client.netPerfNodes() })
        if case .loaded(let list, _, _) = nodes { session.nodesLoaded(list) }
    }
}

/// The pair and duration tested, the progress while running, then how the test ended.
private struct NetPerfStatusSection: View {
    let run: NetPerfSession.Run
    let stop: () -> Void
    let reset: () -> Void

    var body: some View {
        let setup = run.setup
        Section {
            Text("\(setup.client) → \(setup.server) · \(setup.seconds) s")
                .font(.subheadline.monospaced())
            if run.running {
                ProgressView(value: Double(run.results.count), total: Double(setup.steps))
                (run.stopping ? Text("Stopping: deleting the test namespace…") : phaseText)
                    .font(.callout)
                if run.progress?.phase != NetPerfPhase.cleaning {
                    Text("Keep this screen open: leaving it stops the test.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Button("Stop", role: .destructive, action: stop)
                    .disabled(run.stopping)
            } else {
                if let error = run.error {
                    Text("The test failed: \(error)").foregroundStyle(.red)
                }
                if run.stopped { Text("Test stopped.") }
                Button("New test", action: reset)
            }
        }
    }

    private var phaseText: Text {
        guard let p = run.progress else { return Text("Preparing the test namespace…") }
        switch p.phase {
        case NetPerfPhase.starting:
            // "node: reason" while a pod waits, e.g. for its image.
            return p.message.contains(":") ? Text("Waiting for \(p.message)…") : Text("Starting netserver on \(run.setup.server)…")
        case NetPerfPhase.testing:
            return Text("\(netPerfPathLabel(p.path)) · \(netPerfTestLabel(p.test)) (\(p.step)/\(p.steps))…")
        case NetPerfPhase.cleaning:
            return Text("Deleting the test namespace…")
        default:
            return Text("Preparing the test namespace…")
        }
    }
}

/// One section per network path, with its throughput and latency once measured.
private struct NetPerfResultsSections: View {
    let setup: NetPerfSetup
    let results: [NetPerfResult]
    let running: Bool

    var body: some View {
        ForEach(setup.paths, id: \.self) { path in
            let measured = results.filter { $0.path == path }
            if !measured.isEmpty || running {
                Section(netPerfPathLabel(path)) {
                    ForEach([NetPerfTest.throughput, NetPerfTest.latency], id: \.self) { test in
                        let result = measured.first { $0.test == test }
                        if result != nil || running { NetPerfResultRow(test: test, result: result) }
                    }
                }
            }
        }
    }
}

private struct NetPerfResultRow: View {
    let test: String
    /// nil while not measured yet.
    let result: NetPerfResult?

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: netPerfTestLabel(test)).foregroundStyle(.secondary)
                Spacer()
                Text(verbatim: value)
                    .font(.title3.weight(.semibold))
                    .monospacedDigit()
            }
            if let result {
                if !result.error.isEmpty {
                    Text(verbatim: result.error).font(.caption).foregroundStyle(.red)
                } else if test == NetPerfTest.latency, let l = result.latency {
                    let rate = result.transactionRate.formatted(.number.precision(.fractionLength(0)))
                    (Text("p50 \(formatMicros(l.p50)) · p90 \(formatMicros(l.p90)) · p99 \(formatMicros(l.p99))") +
                        Text(verbatim: "  ·  ") + Text("\(rate) round trips/s"))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
            }
        }
    }

    private var value: String {
        guard let result else { return "…" }
        if !result.error.isEmpty { return "—" }
        if test == NetPerfTest.throughput { return formatMbps(result.throughputMbps) }
        return result.latency.map { formatMicros($0.p50) } ?? "—"
    }
}

private func netPerfPathLabel(_ path: String) -> String {
    path == NetPerfPath.host ? String(localized: "Host to host") : String(localized: "Pod to pod")
}

private func netPerfTestLabel(_ test: String) -> String {
    test == NetPerfTest.latency ? String(localized: "Latency (p50)") : String(localized: "Throughput")
}
