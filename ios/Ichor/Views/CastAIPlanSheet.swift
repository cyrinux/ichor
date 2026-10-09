import SwiftUI
import IchorCore

/// One consolidation: why it failed, what it costs before and after, each node it removes with
/// the steps it went through (cordoned, blocked by a budget, deleted or given back), the nodes it
/// adds and the NodePool disruption budgets that pace it.
struct CastAIPlanSheet: View {
    let plan: CastAIPlan

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section {
                    header
                    if plan.planState == .failed || plan.planState == .skipped, !cause.isEmpty {
                        CastAIBanner(text: cause, bad: true)
                            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                    }
                    ForEach(Array(plan.warnings.enumerated()), id: \.offset) { _, warning in
                        CastAIBanner(text: warning)
                            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                    }
                }
                Section { cost }
                if !plan.removing.isEmpty {
                    Section(String(localized: "Removing · \(plan.removing.count)")) {
                        ForEach(plan.removing) { CastAIRemovedNode(node: $0) }
                    }
                }
                if !plan.adding.isEmpty {
                    Section(String(localized: "Adding · \(plan.adding.count)")) {
                        ForEach(plan.adding) { CastAIAddedNode(node: $0, currency: plan.currency) }
                    }
                }
                if !plan.budgets.isEmpty {
                    Section("Disruption budgets") {
                        ForEach(plan.budgets) { CastAIBudgetRow(budget: $0) }
                    }
                }
            }
            .themedBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    /// The failure in our words, then CAST AI's message when it adds something.
    private var cause: String {
        var lines: [String] = []
        for line in [plan.failureText, plan.message] where !line.isEmpty && !lines.contains(line) { lines.append(line) }
        return lines.joined(separator: "\n")
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: [String(localized: "Consolidation"), plan.mode.label].compactMap { $0 }.joined(separator: " · "))
                    .font(.headline)
                Text(verbatim: plan.name).font(.subheadline.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
            ChipFlow(spacing: 8) {
                StatusPill(label: plan.planState.label, color: plan.planState.color)
                if plan.execute { StatusPill(label: String(localized: "Auto-executed"), color: .secondary) }
                StatusPill(label: started, color: .secondary)
            }
        }
    }

    /// "Started 18:29 · ran 4m 12s", without the run time while it runs.
    private var started: String {
        let at = castAITime(plan.createdAt)
        guard plan.endedAt > plan.createdAt else { return String(localized: "Started \(at)") }
        return String(localized: "Started \(at) · ran \(castAIDuration((plan.endedAt - plan.createdAt) / 1000))")
    }

    /// Monthly cost of the nodes the plan touches, before and after, and what that is for the cluster.
    private var cost: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Monthly cost of the nodes it touches").font(.subheadline.weight(.medium)).foregroundStyle(.secondary)
            HStack(alignment: .lastTextBaseline, spacing: 8) {
                Text(verbatim: formatMoney(plan.beforeMonthly, currency: plan.currency)).font(.headline.monospaced()).foregroundStyle(.secondary)
                Text(verbatim: "→").foregroundStyle(.secondary).accessibilityHidden(true)
                Text(verbatim: formatMoney(plan.afterMonthly, currency: plan.currency)).font(.title2.monospaced())
                Spacer()
                Text(verbatim: "−" + String(format: "%.0f%%", plan.savingsPercent)).font(.subheadline.monospaced()).foregroundStyle(.secondary)
            }
            CastAIBeforeAfterBar(before: plan.beforeMonthly, after: plan.afterMonthly, height: 10)
            let lines = [
                plan.achievedMonthly.map { String(localized: "Measured saving: \(formatMoney($0, currency: plan.currency))/month") },
                plan.clusterSharePercent.map {
                    String(localized: "Planned saving \(formatMoney(plan.plannedMonthly, currency: plan.currency))/month, \(String(format: "%.1f%%", $0)) of the cluster")
                },
            ].compactMap { $0 }
            if !lines.isEmpty {
                Text(verbatim: lines.joined(separator: "\n")).font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 6)
    }
}

/// A node's status dot, its name (or instance type) and its status.
private struct CastAINodeTitle: View {
    let node: CastAIPlanNode
    let title: String

    var body: some View {
        HStack(spacing: 10) {
            Circle().fill(node.status.color).frame(width: 8, height: 8).accessibilityHidden(true)
            Text(verbatim: title).font(.subheadline.monospaced()).lineLimit(1).truncationMode(.middle)
            Spacer()
            Text(verbatim: node.status.label).font(.caption2).foregroundStyle(node.status.color)
        }
    }
}

/// A node the plan removes: its outcome, then each step with its time.
private struct CastAIRemovedNode: View {
    let node: CastAIPlanNode

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            CastAINodeTitle(node: node, title: node.name)
            if node.events.count > 1 || node.status != .success {
                ForEach(Array(node.events.enumerated()), id: \.offset) { CastAIEventLine(event: $0.element) }
            } else if let last = node.events.last {
                Text(verbatim: String(localized: "Removed at \(castAITime(last.at))")).font(.caption2).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }
}

/// A node the plan adds: its type and price, and how long it took to be ready.
private struct CastAIAddedNode: View {
    let node: CastAIPlanNode
    let currency: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            CastAINodeTitle(node: node, title: node.instanceType.or(node.name))
            let details = [
                node.spot ? String(localized: "spot") : nil,
                node.zone.nonEmpty,
                node.priceHourly > 0 ? String(localized: "\(formatMoney(node.priceHourly, currency: currency, decimals: 3))/h") : nil,
            ].compactMap { $0 }
            if !details.isEmpty {
                Text(verbatim: details.joined(separator: " · ")).font(.caption2).foregroundStyle(.secondary)
            }
            if let seconds = node.readySeconds {
                Text(verbatim: String(localized: "Ready in \(castAIDuration(seconds))")).font(.caption2).foregroundStyle(.statusOK)
            } else if let last = node.events.last {
                CastAIEventLine(event: last)
            }
        }
        .padding(.vertical, 2)
    }
}

/// "18:29  ●  NodePool disruption budget exhausted…", CAST AI's own words for the step.
private struct CastAIEventLine: View {
    let event: CastAIPlanEvent

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(verbatim: castAITime(event.at)).font(.caption2.monospaced()).foregroundStyle(.secondary).frame(width: 52, alignment: .leading)
            Circle().fill(event.tone.color).frame(width: 7, height: 7).accessibilityHidden(true)
            Text(verbatim: event.text).font(.caption2).foregroundStyle(event.tone.color)
        }
        .padding(.leading, 18)
    }
}

private struct CastAIBudgetRow: View {
    let budget: CastAINodeBudget

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(verbatim: budget.nodePool).font(.subheadline.monospaced())
                Spacer()
                Text(verbatim: String(localized: "\(String(budget.nodes)) nodes")).font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: String(localized: "\(String(budget.allowed)) may be disrupted at a time · \(String(budget.disrupting)) now"))
                .font(.caption)
                .foregroundStyle(budget.exhausted ? Color.statusWarn : Color.secondary)
        }
        .padding(.vertical, 2)
    }
}
