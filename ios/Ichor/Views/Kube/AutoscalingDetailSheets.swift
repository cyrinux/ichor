import SwiftUI
import IchorCore

/// The detail sheet of the row a tap opened: one per kind, the content the screen's rows hint.
struct AutoscalingDetailSheets: View {
    let selection: AutoscalingView.Selection
    /// Reads the report again after an action: the open sheet shows its outcome.
    let onChanged: () async -> Void

    var body: some View {
        switch selection {
        case .hpa(let row): AutoscaleSheet(row: row, onChanged: onChanged)
        case .scaledObject(let row): ScaledObjectSheet(row: row, onChanged: onChanged)
        case .scaledJob(let row): ScaledJobSheet(row: row)
        case .vpa(let row): VpaSheet(row: row)
        case .pool(let row, let claims): NodePoolSheet(row: row, claims: claims)
        }
    }
}

/// One HPA: its metrics against their targets, the three conditions, and what it drives.
struct AutoscaleSheet: View {
    let row: HpaRow
    let onChanged: () async -> Void

    @State private var editingBounds = false

    var body: some View {
        AutoscalingSheetShell {
            Section { header }
            Section("Metrics") {
                if row.metrics.isEmpty {
                    Text("No metrics configured.").foregroundStyle(.secondary)
                }
                ForEach(row.metrics) { metric in
                    LabeledContent(metric.label) {
                        Text(verbatim: AutoscaleText.metricValues(metric.current, metric.target)).monospaced()
                    }
                }
            }
            Section("Conditions") {
                condition("Able to scale", row.conditions.ableToScale)
                condition("Scaling active", row.conditions.scalingActive)
                condition("Scaling limited", row.conditions.scalingLimited)
                if !row.conditions.message.isEmpty {
                    Text(verbatim: row.conditions.message).font(.footnote).foregroundStyle(.secondary)
                }
            }
            boundsSection
            workloadLink
        }
        .loadsKubeActionAccess(namespace: row.namespace)
        .sheet(isPresented: $editingBounds) { HpaBoundsSheet(row: row, onDone: onChanged) }
    }

    private var header: some View {
        AutoscalingSheetHeader(
            name: row.name, line: [row.namespace, row.target.label].filter { !$0.isEmpty }.joined(separator: "  ·  "),
            state: row.state
        ) {
            Text(hpaBoundsText(row.current, row.min, row.max)).font(.callout)
            if row.desired != row.current {
                Text(AutoscaleText.desired(String(row.desired))).font(.callout).foregroundStyle(.secondary)
            }
            if row.lastScaleAt > 0 {
                Text(AutoscaleText.lastScaled(relativeTime(row.lastScaleAt))).font(.footnote).foregroundStyle(
                    .secondary)
            }
            if !row.managedBy.isEmpty {
                Text(AutoscaleText.managedBy(row.managedBy, row.scaledObject)).font(.footnote).foregroundStyle(
                    .secondary)
            }
        }
    }

    /// Edit min/max, gated on patch; an HPA KEDA owns says where its bounds live instead.
    private var boundsSection: some View {
        Section("Bounds") {
            if row.isKEDAOwned {
                Text(AutoscaleText.kedaOwned(row.scaledObject)).font(.footnote).foregroundStyle(.secondary)
            } else {
                Button("Edit min/max", systemImage: "arrow.up.and.down") { editingBounds = true }
                    .kubeGated(.autoscale)
                KubeDeniedNote(.autoscale)
            }
        }
    }

    @ViewBuilder
    private func condition(_ label: String, _ ok: Bool) -> some View {
        LabeledContent(label) {
            Text(ok ? "yes" : "no").foregroundStyle(ok ? .primary : Color.statusBad)
        }
    }

    @ViewBuilder
    private var workloadLink: some View {
        if let resource = row.target.resource {
            NavigationLink {
                KubeObjectView(resource: resource, namespace: row.namespace, name: row.target.name)
            } label: {
                Label(AutoscaleText.openWorkload(row.target.label), systemImage: "arrow.up.forward.app")
            }
        }
    }
}

/// One KEDA ScaledObject: its triggers, its fallback, and the HPA it owns.
struct ScaledObjectSheet: View {
    let row: ScaledObjectRow
    let onChanged: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var pausing = false
    @State private var confirmResume = false
    @State private var resuming = false
    @State private var failure: String?

    var body: some View {
        AutoscalingSheetShell {
            Section { header }
            Section("Triggers") {
                if row.triggers.isEmpty {
                    Text("none").foregroundStyle(.secondary)
                }
                ForEach(row.triggers) { trigger in
                    Text(verbatim: trigger.label).font(.callout.monospaced())
                }
            }
            if !row.hpaName.isEmpty {
                Section("The HPA it owns") {
                    Text(verbatim: row.hpaName).font(.callout.monospaced())
                }
            }
            pauseSection
            workloadLink
        }
        .loadsKubeActionAccess(namespace: row.namespace)
        .sheet(isPresented: $pausing) { ScaledObjectPauseSheet(row: row, onDone: onChanged) }
        .confirmationDialog(AutoscaleText.resumeTitle(row.name), isPresented: $confirmResume, titleVisibility: .visible)
        {
            Button("Resume scaling") { Task { await resume() } }
        } message: {
            Text("KEDA scales the workload on its triggers again.")
        }
    }

    /// Pause at a count or hold (a sheet), resume when paused (confirmed), gated on patch.
    private var pauseSection: some View {
        Section("Pause") {
            Button("Pause at…", systemImage: "pause.circle") { pausing = true }
                .kubeGated(.autoscaleKeda)
                .disabled(resuming)
            if row.paused {
                Button {
                    confirmResume = true
                } label: {
                    HStack {
                        Label("Resume scaling", systemImage: "play.circle")
                        if resuming { Spacer(); ProgressView() }
                    }
                }
                .kubeGated(.autoscaleKeda)
                .disabled(resuming)
            }
            KubeDeniedNote(.autoscaleKeda)
            if let failure {
                Text(verbatim: failure).font(.footnote).foregroundStyle(.statusBad).textSelection(.enabled)
            }
        }
    }

    private func resume() async {
        guard let client = model.client else { return }
        resuming = true
        failure = nil
        do {
            try await client.pauseScaledObject(
                namespace: row.namespace, name: row.name, replicas: ScaledObjectPause.resume)
            await onChanged()
        } catch {
            failure = error.localizedDescription
        }
        resuming = false
    }

    private var header: some View {
        AutoscalingSheetHeader(
            name: row.name, line: [row.namespace, row.target.label].filter { !$0.isEmpty }.joined(separator: "  ·  "),
            state: row.state
        ) {
            Text(hpaBoundsText(row.current, row.min, row.max)).font(.callout)
            if row.paused {
                Text(row.pausedReplicas.map { AutoscaleText.pausedAt(String($0)) } ?? "paused")
                    .font(.callout).foregroundStyle(.purple)
            }
            if row.fallback {
                Text(AutoscaleText.fallbackReplicas(String(row.fallbackReplicas)))
                    .font(.callout).foregroundStyle(Color.statusWarn)
            }
            if !row.message.isEmpty {
                Text(verbatim: row.message).font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private var workloadLink: some View {
        if let resource = row.target.resource {
            NavigationLink {
                KubeObjectView(resource: resource, namespace: row.namespace, name: row.target.name)
            } label: {
                Label(AutoscaleText.openWorkload(row.target.label), systemImage: "arrow.up.forward.app")
            }
        }
    }
}

/// One KEDA ScaledJob: its triggers and its ceiling.
struct ScaledJobSheet: View {
    let row: ScaledJobRow

    var body: some View {
        AutoscalingSheetShell {
            Section {
                AutoscalingSheetHeader(name: row.name, line: row.namespace, state: row.state) {
                    if row.paused {
                        Text("paused").font(.callout).foregroundStyle(.purple)
                    }
                    if !row.message.isEmpty {
                        Text(verbatim: row.message).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            }
            Section("Triggers") {
                if row.triggers.isEmpty {
                    Text("none").foregroundStyle(.secondary)
                }
                ForEach(row.triggers) { trigger in
                    Text(verbatim: trigger.label).font(.callout.monospaced())
                }
            }
            Section {
                LabeledContent("Max replicas") { Text(verbatim: String(row.maxReplicaCount)) }
            }
        }
    }
}

/// One VPA: what the recommender wants per container, against the live requests.
struct VpaSheet: View {
    let row: VpaRow

    var body: some View {
        AutoscalingSheetShell {
            Section { header }
            if !row.recommendationProvided {
                Section {
                    Text("No recommendation yet: the recommender needs history first.").foregroundStyle(.secondary)
                    if !row.message.isEmpty {
                        Text(verbatim: row.message).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            }
            if !row.containers.isEmpty {
                Section("Recommendations") {
                    ForEach(row.containers) { container in
                        VStack(alignment: .leading, spacing: 3) {
                            Text(verbatim: container.name).font(.callout.monospaced())
                            Text(AutoscaleText.cpuChange(vpaChangeText(container.cpuRequest, container.cpuTarget)))
                                .font(.callout)
                            Text(
                                AutoscaleText.memoryChange(
                                    vpaChangeText(container.memoryRequest, container.memoryTarget))
                            ).font(.callout)
                            Text(
                                AutoscaleText.bounds(
                                    container.cpuLower, container.cpuUpper, container.memoryLower, container.memoryUpper
                                )
                            )
                            .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
            workloadLink
        }
    }

    private var header: some View {
        AutoscalingSheetHeader(
            name: row.name, line: [row.namespace, row.target.label].filter { !$0.isEmpty }.joined(separator: "  ·  "),
            state: row.state
        ) {
            if !row.updateMode.isEmpty {
                LabeledContent("Update mode") { Text(verbatim: row.updateMode) }
            }
        }
    }

    @ViewBuilder
    private var workloadLink: some View {
        if let resource = row.target.resource {
            NavigationLink {
                KubeObjectView(resource: resource, namespace: row.namespace, name: row.target.name)
            } label: {
                Label(AutoscaleText.openWorkload(row.target.label), systemImage: "arrow.up.forward.app")
            }
        }
    }
}

/// One Karpenter NodePool: its limits against its usage, its disruption budgets, its claims.
struct NodePoolSheet: View {
    let row: NodePoolRow
    let claims: [NodeClaimRow]

    var body: some View {
        AutoscalingSheetShell {
            Section { header }
            Section("Limits and usage") {
                LabeledContent("cpu") {
                    Text(verbatim: AutoscaleText.poolUsage(row.resources.cpu, row.limits.cpu)).monospaced()
                }
                LabeledContent("memory") {
                    Text(verbatim: AutoscaleText.memoryUsage(row.resources.memory, row.limits.memory)).monospaced()
                }
                LabeledContent("nodes") { Text(verbatim: String(row.resources.nodes)) }
            }
            Section("Disruption") {
                if !row.disruption.consolidationPolicy.isEmpty {
                    LabeledContent("Policy") { Text(verbatim: row.disruption.consolidationPolicy) }
                }
                if !row.disruption.consolidateAfter.isEmpty {
                    Text(AutoscaleText.consolidateAfter(row.disruption.consolidateAfter)).font(.callout)
                }
                ForEach(row.disruption.budgets) { budget in
                    Text(verbatim: budgetLine(budget)).font(.callout.monospaced())
                }
            }
            Section("Node claims") {
                if claims.isEmpty {
                    Text("none").foregroundStyle(.secondary)
                }
                ForEach(claims) { claim in
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(verbatim: claim.name).font(.callout.monospaced())
                            Spacer()
                            NodeClaimBadges(claim: claim)
                        }
                        Text(
                            verbatim: [claim.nodeName, claim.instanceType, claim.capacityType]
                                .filter { !$0.isEmpty }.joined(separator: "  ·  ")
                        )
                        .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        if claim.created > 0 {
                            Text(
                                Date(timeIntervalSince1970: TimeInterval(claim.created) / 1000),
                                format: .relative(presentation: .named)
                            )
                            .font(.caption).foregroundStyle(.secondary)
                        }
                        if !claim.message.isEmpty {
                            Text(verbatim: claim.message).font(.caption)
                        }
                    }
                }
            }
        }
    }

    private var header: some View {
        AutoscalingSheetHeader(name: row.name, line: nil) {
            if row.atLimit {
                AutoscaleChip(text: "at limit", color: .statusBad)
            } else {
                AutoscaleStateBadge(state: row.state)
            }
        } extra: {
            if !row.capacityTypes.isEmpty {
                Text(AutoscaleText.capacityTypes(row.capacityTypes.joined(separator: ", ")))
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if row.weight > 0 {
                LabeledContent("Weight") { Text(verbatim: String(row.weight)) }
            }
            if !row.message.isEmpty {
                Text(verbatim: row.message).font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    /// One disruption budget as one line: its nodes, when, how long, why.
    private func budgetLine(_ budget: NodePoolBudget) -> String {
        [
            AutoscaleText.budgetNodes(budget.nodes), budget.schedule, budget.duration,
            budget.reasons.isEmpty ? nil : budget.reasons.joined(separator: ", "),
        ]
        .compactMap { $0 }.joined(separator: "  ·  ")
    }
}
