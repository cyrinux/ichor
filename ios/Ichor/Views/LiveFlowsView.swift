import SwiftUI
import IchorCore

/// Live Hubble flows for the screen showing them: Go sends the whole view at most once a
/// second, so each update replaces the last one.
@Observable
@MainActor
final class HubbleFollower {
    private(set) var snapshot: HubbleSnapshot?
    private(set) var error: String?
    private(set) var active = false
    /// Every namespace seen since the screen opened, for the filter: kept across restarts.
    private(set) var namespaces: [String] = []

    /// Follows until the stream ends or the task is cancelled (filter changed, screen left).
    func run(_ client: TalosClient, filter: HubbleFilter) async {
        snapshot = nil
        error = nil
        active = true
        defer { active = false }
        for await event in client.hubbleFlows(filter) {
            switch event {
            case .update(let next):
                snapshot = next
                let seen = Set(namespaces).union(next.flowNamespaces)
                if seen.count != namespaces.count { namespaces = seen.sorted() }
            case .done(let failure):
                if !Task.isCancelled { error = failure }
            }
        }
    }
}

/// Cilium's flows live, like Hubble UI (os:admin): `hubble observe --follow` in every agent,
/// merged. Drops come grouped with the policies behind them; a filter change restarts the
/// stream, leaving the screen stops it.
struct LiveFlowsView: View {
    let cilium: CiliumStatus

    enum Tab: Hashable { case drops, flows }

    /// The stream to run: a new one on a filter change or a retry.
    private struct RunKey: Hashable {
        let filter: HubbleFilter
        let attempt: Int
    }

    @Environment(AppModel.self) private var model
    @State private var filter: HubbleFilter
    @State private var attempt = 0
    @State private var tab = Tab.drops
    @State private var follower = HubbleFollower()
    @State private var selected: HubbleDropGroup?

    init(cilium: CiliumStatus, filter: HubbleFilter = HubbleFilter()) {
        self.cilium = cilium
        _filter = State(initialValue: filter)
    }

    var body: some View {
        Group {
            if cilium.hubble {
                flowList
                    .safeAreaInset(edge: .top) { tabBar }
            } else {
                ContentUnavailableView {
                    Label("Hubble is disabled", systemImage: "eye.slash")
                } description: {
                    Text("Cilium runs without Hubble, which records the flows. Set hubble.enabled=true in Cilium’s Helm values (enable-hubble in the cilium-config ConfigMap), then open this screen again.")
                }
                .themedBackground()
            }
        }
        .task(id: RunKey(filter: filter, attempt: attempt)) {
            guard cilium.hubble, let client = model.client else { return }
            await follower.run(client, filter: filter)
        }
        .navigationTitle("Live flows")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $selected) { DropGroupSheet(group: $0) }
    }

    private var tabBar: some View {
        Picker(selection: $tab) {
            Text("Drops (\(follower.snapshot?.drops.count ?? 0))").tag(Tab.drops)
            Text("All flows").tag(Tab.flows)
        } label: {
            EmptyView()
        }
        .pickerStyle(.segmented)
        .padding(.horizontal)
        .padding(.bottom, 6)
        .background(.bar)
    }

    private var flowList: some View {
        List {
            filterSection
            HubbleStatusSection(snapshot: follower.snapshot, buffer: cilium.buffer, error: follower.error,
                                connecting: follower.active && follower.snapshot == nil) { attempt += 1 }
            if let snapshot = follower.snapshot {
                switch tab {
                case .drops:
                    Section {
                        if snapshot.drops.isEmpty {
                            Text("No drops so far.").note()
                        }
                        ForEach(snapshot.drops) { group in
                            Button { selected = group } label: { DropGroupRow(group: group) }
                        }
                    } footer: {
                        if !snapshot.policiesError.isEmpty {
                            Text("Drops are not matched to policies: \(snapshot.policiesError)")
                        }
                    }
                case .flows:
                    Section {
                        if snapshot.flows.isEmpty {
                            Text("No flows so far.").note()
                        }
                        ForEach(Array(snapshot.flows.enumerated()), id: \.offset) { _, flow in
                            HubbleFlowRow(flow: flow)
                        }
                    }
                }
            }
        }
        .themedBackground()
    }

    @ViewBuilder private var filterSection: some View {
        Section {
            Toggle("Drops only", isOn: $filter.dropsOnly)
            Picker("Namespace", selection: namespaceBinding) {
                Text("All namespaces").tag(String?.none)
                ForEach(namespaceChoices, id: \.self) { Text(verbatim: $0).tag(String?.some($0)) }
            }
            if let pod = filter.pod {
                HStack {
                    LabeledContent("Pod") { Text(verbatim: pod).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle) }
                    Button { filter = HubbleFilter(namespace: filter.namespace, dropsOnly: filter.dropsOnly) } label: {
                        Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text("Show the whole namespace"))
                }
            }
        }
    }

    /// The namespaces seen, and the one filtered on (which only its own flows would show).
    private var namespaceChoices: [String] {
        Array(Set(follower.namespaces + [filter.namespace].compactMap { $0 })).sorted()
    }

    /// A new namespace drops the pod filter.
    private var namespaceBinding: Binding<String?> {
        Binding(get: { filter.namespace },
                set: { filter = HubbleFilter(namespace: $0, dropsOnly: filter.dropsOnly) })
    }
}

/// The agents' states, the counters, and how far back the view goes.
private struct HubbleStatusSection: View {
    let snapshot: HubbleSnapshot?
    let buffer: Int
    let error: String?
    let connecting: Bool
    let retry: () -> Void

    var body: some View {
        Section {
            if let error {
                ErrorOrNoticeText(message: error)
                Button("Retry", action: retry)
            } else if connecting {
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Connecting to the Cilium agents…").foregroundStyle(.secondary)
                }
            }
            if let snapshot {
                ChipFlow {
                    ForEach(snapshot.agentsByAttention) { agent in
                        StatusPill(label: "\(agent.node) · \(agent.state.label)", color: agent.state.color)
                    }
                }
                ForEach(snapshot.nodes.filter { $0.state == .error && !$0.error.isEmpty }) { agent in
                    Text(verbatim: "\(agent.node): \(agent.error)").font(.caption).foregroundStyle(.red)
                }
                HStack {
                    counter(String(localized: "Seen"), snapshot.seen, color: .primary)
                    counter(String(localized: "Dropped"), snapshot.dropped, color: snapshot.dropped > 0 ? .red : .primary)
                    counter(String(localized: "Lost"), snapshot.lost, color: snapshot.lost > 0 ? attentionColor : .primary)
                }
            }
        } footer: {
            if buffer > 0 {
                Text("History only goes back as far as the agents still keep: up to \(buffer) flows per node.")
            } else {
                Text("History only goes back as far as the agents still keep.")
            }
        }
    }

    private func counter(_ label: String, _ value: Int64, color: Color) -> some View {
        VStack(spacing: 2) {
            Text(verbatim: "\(value)").font(.title3.weight(.semibold)).monospacedDigit().foregroundStyle(color)
            Text(verbatim: label).font(.caption).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
    }
}

/// "src → dst :port/proto", the verdict, the reason in words, how often, when, where.
private struct DropGroupRow: View {
    let group: HubbleDropGroup

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: "\(group.source.label) → \(group.destination.label)\(group.endpointSuffix)")
                .font(.callout.monospaced())
                .lineLimit(2)
                .truncationMode(.middle)
                .foregroundStyle(.primary)
            HStack(spacing: 6) {
                Text(verbatim: group.verdict.label).foregroundStyle(group.verdict.color)
                if let direction = group.direction {
                    Text(verbatim: direction.label).foregroundStyle(.secondary)
                }
                Text(verbatim: "×\(group.count)").monospacedDigit().foregroundStyle(.secondary)
            }
            .font(.caption.weight(.medium))
            Text(verbatim: group.reason.text).font(.caption).foregroundStyle(.secondary)
            Text(verbatim: ([Date(epochMillis: group.lastSeen).formatted(.relative(presentation: .named))] + group.nodes)
                .joined(separator: " · "))
                .font(.caption2)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }
}

/// One flow, compact: time, verdict color, peers, port, L7 line.
private struct HubbleFlowRow: View {
    let flow: HubbleFlow

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle().fill(flow.verdict.color).frame(width: 8, height: 8)
                .accessibilityLabel(Text(verbatim: flow.verdict.label))
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: "\(flow.source.label) → \(flow.destination.label)")
                    .font(.caption.monospaced())
                    .lineLimit(2)
                    .truncationMode(.middle)
                HStack(spacing: 6) {
                    Text(Date(epochMillis: flow.time), format: .dateTime.hour().minute().second())
                    if !flow.portLabel.isEmpty { Text(verbatim: flow.portLabel) }
                    if flow.verdict != .forwarded {
                        Text(verbatim: flow.verdict.label).foregroundStyle(flow.verdict.color)
                    }
                }
                .font(.caption2)
                .foregroundStyle(.secondary)
                if !flow.l7.isEmpty {
                    Text(verbatim: flow.l7).font(.caption2.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
    }
}
