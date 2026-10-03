import SwiftUI
import IchorCore

/// A network test between two nodes (netperf, like `cilium connectivity perf`): the setup,
/// then its progress and the measurements. The session belongs to the screen showing it
/// (Kubernetes or Cluster insights), which stops a running test when it goes away.
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
                } else if let report = session.viewing {
                    savedSections(report)
                } else {
                    setupSections(nodes.filter(\.ready))
                    historySection
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task {
            if let cluster = model.activeSummary?.fingerprint {
                session.loadHistory(scope: "\(cluster)-\(model.privacyStorageKey)")
            }
            await load()
        }
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
            } header: {
                HintedHeader(title: Text("Client and server"), hint: NetPerfHint.nodes)
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
            } header: {
                HintedHeader(title: Text("Network paths"), hint: NetPerfHint.paths)
            }
            Section {
                Picker(selection: $session.setup.seconds) {
                    ForEach(netPerfDurations, id: \.self) { seconds in Text("\(seconds) s").tag(seconds) }
                } label: {
                    EmptyView()
                }
                .pickerStyle(.segmented)
            } header: {
                HintedHeader(title: Text("Duration of each measurement"), hint: NetPerfHint.duration)
            }
            Section {
                Button("Start test") { confirming = true }
                    .disabled(!session.setup.ready)
            }
        }
    }

    /// The saved tests under the setup, newest first; one opens its results.
    @ViewBuilder
    private var historySection: some View {
        if !session.history.isEmpty {
            Section {
                ForEach(session.history, id: \.started) { report in
                    Button { session.viewing = report } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(report.client) → \(report.server) · \(report.seconds) s")
                                .font(.subheadline.monospaced())
                                .foregroundStyle(.primary)
                            Text(verbatim: ([testedAt(report)] + [headline(report)].compactMap { $0 }).joined(separator: "  ·  "))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .onDelete { offsets in offsets.map { session.history[$0] }.forEach(session.delete) }
            } header: {
                Text("Previous tests")
            } footer: {
                Text("Kept encrypted on this phone: the last \(netPerfHistoryLimit) tests of this cluster.")
            }
        }
    }

    /// A saved test: when it ran and what it measured.
    @ViewBuilder
    private func savedSections(_ report: NetPerfReport) -> some View {
        Section {
            Text("\(report.client) → \(report.server) · \(report.seconds) s")
                .font(.subheadline.monospaced())
            Text(verbatim: testedAt(report)).font(.caption).foregroundStyle(.secondary)
            Button("Back") { session.viewing = nil }
            Button("Delete", role: .destructive) { session.delete(report) }
        }
        NetPerfResultsSections(setup: report.setup, results: report.results, running: false)
    }

    private func testedAt(_ report: NetPerfReport) -> String {
        Date(timeIntervalSince1970: Double(report.started) / 1000).formatted(date: .abbreviated, time: .shortened)
    }

    /// Pod-to-pod throughput and p50 latency, the figures the list compares tests by.
    private func headline(_ report: NetPerfReport) -> String? {
        let pod = report.results.filter { $0.path == NetPerfPath.pod && $0.error.isEmpty }
        let figures = [
            pod.first { $0.test == NetPerfTest.throughput }.map { formatMbps($0.throughputMbps) },
            pod.first { $0.test == NetPerfTest.latency }?.latency.map { formatMicros($0.p50) },
        ].compactMap { $0 }
        return figures.isEmpty ? nil : figures.joined(separator: " · ")
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
                Section {
                    ForEach([NetPerfTest.throughput, NetPerfTest.latency], id: \.self) { test in
                        let result = measured.first { $0.test == test }
                        if result != nil || running { NetPerfResultRow(test: test, result: result) }
                    }
                } header: {
                    HintedHeader(title: Text(verbatim: netPerfPathLabel(path)), hint: NetPerfHint.paths)
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
                InfoHint(
                    title: Text(verbatim: netPerfTestLabel(test)),
                    text: Text(test == NetPerfTest.latency ? NetPerfHint.latency : NetPerfHint.throughput)
                )
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

/// What the (i) next to each part of the test explains.
private enum NetPerfHint {
    static let nodes: LocalizedStringKey = "The server node runs netserver and waits. The client node runs netperf, which connects to it and sends: throughput is measured from the client to the server.\n\nSwap the two nodes to measure the other direction. Choosing the same node twice measures its own network stack, not a link."
    static let paths: LocalizedStringKey = "Pod to pod goes through the CNI: virtual interfaces, overlay or encapsulation, eBPF, encryption such as WireGuard or KubeSpan. Host to host uses the nodes’ own addresses and skips all of it.\n\nPod to pod much slower than host to host: the gap is what the CNI costs. Both slow: look at the link itself (NIC, switch, MTU, distance between sites)."
    static let duration: LocalizedStringKey = "How long each measurement lasts. Longer runs smooth out bursts and TCP’s ramp-up, but keep the link busy for longer.\n\nThe test makes 2 measurements one after the other, throughput then latency, and 4 with the host network."
    static let throughput: LocalizedStringKey = "netperf TCP_STREAM: one TCP connection sends as fast as it can for the whole duration, so this is the bandwidth a single flow gets.\n\nOn a healthy path it comes close to the link speed: about 940 Mbit/s on 1 GbE, 9.4 Gbit/s on 10 GbE. Encryption, a small MTU or a busy CPU lower it. Several flows together can reach more."
    static let latency: LocalizedStringKey = "netperf TCP_RR: one byte goes to the server and back, again and again on one connection. Each value is a full round trip, both network stacks included.\n\np50 is the median: half of the round trips were faster. p90: 9 in 10 were faster. p99: only the slowest 1 in 100 took longer, where jitter and retransmits show. A p99 far above p50 means an uneven link.\n\nRound trips/s is how many completed each second, one after the other. On a LAN expect tens to a few hundred µs; between sites, milliseconds."
}

/// A section title with its (i).
private struct HintedHeader: View {
    let title: Text
    let hint: LocalizedStringKey

    var body: some View {
        HStack(spacing: 4) {
            title
            InfoHint(title: title, text: Text(hint))
        }
    }
}
