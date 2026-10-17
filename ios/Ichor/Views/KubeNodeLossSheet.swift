import SwiftUI
import IchorCore

/// What losing a node — or several picked together — costs: where their pods land, what they
/// evict, what stays Pending, then what to scale down, lowest priority first (system classes
/// never). Opened on the worst node (empty selection), it keeps the other candidates to
/// compare, several at a time; a suggestion opens the workload's scale sheet, nothing is
/// scaled from here.
struct KubeNodeLossSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var selected: Set<String>
    @State private var state: LoadState<NodeLossPlan> = .loading
    /// The worst case's candidates and node, kept while other nodes are picked.
    @State private var candidates: [NodeLossCandidate] = []
    @State private var worst = ""
    @State private var scaling: NodeLossSuggestion?

    init(start: String) {
        _selected = State(initialValue: start.isEmpty ? [] : [start])
    }

    /// The shown plan's node or nodes (as on Android), so ticking nodes before Simulate does not
    /// retitle the previous plan; "What if a node goes down" or the worst node before a plan.
    private var titleText: String {
        guard case .loaded(let plan, _, _) = state else {
            return worst.isEmpty ? CapacityText.capacityWhatIf : CapacityText.capacityLossTitle(worst)
        }
        return plan.nodes.count > 1 ? CapacityText.capacityLossTitleMulti(plan.node) : CapacityText.capacityLossTitle(plan.node)
    }

    var body: some View {
        NavigationStack {
            LoadStateView(state: state, retry: { Task { await load() } }) { plan in
                List {
                    summary(plan)
                    if !plan.preempted.isEmpty { preempted(plan) }
                    pending(plan)
                    if !plan.suggestions.isEmpty { suggestions(plan) }
                    Section {
                        ForEach(plan.assumptions, id: \.self) { Text(verbatim: $0).font(.caption).foregroundStyle(.secondary) }
                    } header: {
                        Text(verbatim: CapacityText.capacityLossEstimate)
                    }
                }
            }
            .navigationTitle(Text(verbatim: titleText))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } }
            }
            // One node (or none: the worst case) simulates as soon as it is picked; several
            // wait for the Simulate button.
            .task(id: selected) { if selected.count <= 1 { await load() } }
            .sheet(item: $scaling) { s in
                if let resource = s.scaleResource {
                    CapacityScaleSheet(resource: resource, namespace: s.namespace, name: s.name)
                }
            }
        }
    }

    private func toggle(_ name: String) {
        if selected.contains(name) { selected.remove(name) } else { selected.insert(name) }
    }

    @ViewBuilder
    private func summary(_ plan: NodeLossPlan) -> some View {
        Section {
            if candidates.count > 1 {
                Text(verbatim: CapacityText.capacityLossWorst(worst)).font(.callout)
                ChipFlow(spacing: 8) {
                    ForEach(candidates) { c in
                        Button { toggle(c.name) } label: {
                            HStack(spacing: 6) {
                                Image(systemName: selected.contains(c.name) ? "checkmark.circle.fill" : "circle")
                                Text(verbatim: CapacityText.capacityLossCandidate(c.name, "\(c.pending)"))
                            }
                            .font(.caption.weight(.medium))
                            .padding(.horizontal, 10)
                            .padding(.vertical, 4)
                            .background(
                                selected.contains(c.name) ? Color.statusOK.opacity(0.15) : Color.secondary.opacity(0.12),
                                in: Capsule())
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(selected.contains(c.name) ? Color.statusOK : Color.primary)
                        .accessibilityAddTraits(selected.contains(c.name) ? [.isSelected] : [])
                    }
                }
                if selected.count > 1 {
                    Button { Task { await load() } } label: {
                        Text(verbatim: CapacityText.capacityLossSimulate("\(selected.count)"))
                    }
                    .buttonStyle(.borderedProminent)
                }
            }
            Text(verbatim: CapacityText.capacityLossLost("\(plan.podsLost)", formatCPU(plan.cpuLost), formatBytes(Int64(plan.memoryLost)), "\(plan.daemonSetPods)"))
                .font(.callout).foregroundStyle(.secondary)
            Text(verbatim: CapacityText.capacityLossPlaced("\(plan.placed.count)"))
        }
    }

    private func preempted(_ plan: NodeLossPlan) -> some View {
        Section {
            ForEach(plan.preempted) { p in
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: CapacityText.capacityLossPreemptedRow("\(p.namespace)/\(p.name)", p.by)).font(.callout.monospaced())
                    if p.pdbViolated {
                        Text(verbatim: CapacityText.capacityLossPdbViolated).font(.caption).foregroundStyle(.statusWarn)
                    }
                }
            }
        } header: {
            Text(verbatim: CapacityText.capacityLossPreemptedTitle)
        }
    }

    @ViewBuilder
    private func pending(_ plan: NodeLossPlan) -> some View {
        if plan.pending.isEmpty {
            Section { Text(verbatim: CapacityText.capacityLossAllFit).foregroundStyle(.statusOK) }
        } else {
            Section {
                ForEach(plan.pending) { p in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: "\(p.namespace)/\(p.name)").font(.callout.monospaced())
                        Text(verbatim: Self.reasonText(p.reason)).font(.caption).foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
            } header: {
                Text(verbatim: CapacityText.capacityLossPendingTitle)
            } footer: {
                Text(verbatim: CapacityText.capacityLossSystemNote)
            }
        }
    }

    private func suggestions(_ plan: NodeLossPlan) -> some View {
        Section {
            ForEach(plan.suggestions) { s in
                Button { scaling = s } label: { SuggestionRow(suggestion: s) }
                    .buttonStyle(.plain)
                    .disabled(s.scaleResource == nil || s.pdbBlocks)
                    .accessibilityHint(Text(verbatim: CapacityText.capacityLossOpenScale))
            }
        } header: {
            Text(verbatim: CapacityText.capacityLossSuggestionsTitle)
        } footer: {
            if !plan.covered {
                Text(verbatim: CapacityText.capacityLossNotEnough).foregroundStyle(.statusWarn)
            }
        }
    }

    private static func reasonText(_ reason: String) -> String {
        switch reason {
        case "cpu": CapacityText.capacityLossPendingCpu
        case "memory": CapacityText.capacityLossPendingMemory
        case "ephemeral": CapacityText.capacityLossPendingEphemeral
        default: CapacityText.capacityLossPendingPods
        }
    }

    /// Reads the plan of the selected nodes; the previous plan stays on screen meanwhile, and
    /// an answer for a selection no longer picked (the Go call cannot be interrupted) is dropped.
    private func load() async {
        guard let client = model.client else { return }
        let asked = selected
        let loaded: LoadState<NodeLossPlan> = await .from { try await client.nodeLoss(nodes: asked.sorted()) }
        guard !Task.isCancelled, asked == selected else { return }
        if case .loaded(let plan, _, _) = loaded, plan.worstCase {
            candidates = plan.candidates
            worst = plan.node
        }
        state = state.refreshed(with: loaded)
    }
}

/// "Scale Kind ns/name from 3 to 2", its class, priority, what it frees and its budget.
private struct SuggestionRow: View {
    let suggestion: NodeLossSuggestion

    var body: some View {
        let s = suggestion
        let subject = "\(s.kind) \(s.namespace)/\(s.name)"
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: s.pdbBlocks ? subject : CapacityText.capacityLossSuggestion(subject, "\(s.replicas)", "\(s.scaleTo)"))
                .font(.callout.monospaced())
            Text(verbatim: CapacityText.capacityLossSuggestionDetail(
                s.noPriorityClass ? CapacityText.capacityNoClass : s.priorityClassName,
                "\(s.priority)",
                formatCPU(s.freesCPU),
                formatBytes(Int64(s.freesMemory))))
                .font(.caption).foregroundStyle(.secondary)
            if s.pdbBlocks {
                Text(verbatim: CapacityText.capacityLossPdbBlocks).font(.caption).foregroundStyle(.statusWarn)
            } else if !s.pdb.isEmpty {
                Text(verbatim: CapacityText.capacityLossPdbMin("\(s.pdbMinAvailable)")).font(.caption).foregroundStyle(.secondary)
            }
            if s.scaleResource == nil {
                Text(verbatim: CapacityText.capacityLossNoScale).font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

/// The resource browser's scale sheet for a suggested workload, once the credentials were asked
/// whether they may scale it (an answer that cannot be had leaves Apply on).
private struct CapacityScaleSheet: View {
    let resource: KubeAPIResource
    let namespace: String
    let name: String
    @Environment(AppModel.self) private var model
    @State private var denial: KubeAccess?

    var body: some View {
        KubeObjectScaleSheet(resource: resource, namespace: namespace, name: name, denial: denial) {}
            .task {
                guard let client = model.client,
                      let access = try? await client.kubeCan(verb: "patch", group: resource.group, resource: resource.scaleResource,
                                                             namespace: namespace, name: name) else { return }
                denial = access.isDenied ? access : nil
            }
    }
}
