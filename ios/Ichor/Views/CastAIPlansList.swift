import SwiftUI
import IchorCore

/// CAST AI's node consolidations (RebalancePlans): nodes it keeps failing to remove first, then
/// what the last day saved and missed, then each plan by state with its cost before and after.
struct CastAIPlansList: View {
    let status: CastAIStatus
    let refresh: () async -> Void

    @State private var selected: CastAIPlan?

    var body: some View {
        let scale = status.planCostScale
        List {
            if !status.plansError.isEmpty { Section { ErrorLine(error: status.plansError) } }
            if status.plans.isEmpty && status.plansError.isEmpty {
                Section { Text("No node consolidation yet.").foregroundStyle(.secondary) }
            } else {
                if !status.stuck.isEmpty {
                    Section {
                        ForEach(status.stuck) { stuck in
                            CastAIBanner(text: stuckText(stuck), bad: true)
                                .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                        }
                    }
                }
                if !status.plans.isEmpty {
                    Section { CastAIPlansSummary(status: status) }
                }
                ForEach(status.planGroups) { group in
                    Section {
                        ForEach(group.plans) { plan in
                            Button { selected = plan } label: { CastAIPlanRow(plan: plan, scale: scale) }
                                .buttonStyle(.plain)
                        }
                    } header: {
                        Text(verbatim: group.state.label).foregroundStyle(group.state.color).textCase(nil)
                    }
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
        .sheet(item: $selected) { CastAIPlanSheet(plan: $0) }
    }

    private func stuckText(_ stuck: CastAIStuckNode) -> String {
        let failures = String(stuck.failures)
        return stuck.retrying
            ? String(localized: "\(stuck.node) could not be drained (\(failures) failed plans); a running plan is trying again.")
            : String(localized: "\(stuck.node) could not be drained (\(failures) failed plans) and a later plan tried it again.")
    }
}

/// The last day's plans: what the finished ones saved, what the failed ones did not, by state.
private struct CastAIPlansSummary: View {
    let status: CastAIStatus

    var body: some View {
        let s = status.planSummary(now: Date().epochMillis)
        VStack(alignment: .leading, spacing: 14) {
            Text("Last 24 hours").font(.subheadline.weight(.medium)).foregroundStyle(.secondary)
            HStack(alignment: .top, spacing: 12) {
                CastAIMetricTile(title: String(localized: "Saved"),
                                 value: castAIApprox(formatMoney(s.savedMonthly, currency: s.currency), estimated: s.savedEstimated),
                                 caption: String(localized: "per month · \(String(s.done)) done"), color: .statusOK)
                CastAIMetricTile(title: String(localized: "Not achieved"),
                                 value: castAIApprox(formatMoney(s.missedMonthly, currency: s.currency), estimated: s.missedEstimated),
                                 caption: String(localized: "per month · \(String(s.failed)) failed"),
                                 color: s.failed > 0 ? .statusBad : .secondary)
            }
            CastAISplitBar(parts: [
                CastAISplitPart(count: s.done, color: .statusOK, label: String(localized: "\(s.done) done")),
                CastAISplitPart(count: s.failed, color: .statusBad, label: String(localized: "\(s.failed) failed")),
                CastAISplitPart(count: s.running, color: .accentColor, label: String(localized: "\(s.running) running")),
                CastAISplitPart(count: s.other, color: .gray, label: String(localized: "\(s.other) other")),
            ])
            if s.clusterNodes > 0 {
                Text(verbatim: String(localized: "cluster \(formatMoney(s.clusterMonthly, currency: s.currency))/month · \(String(s.clusterNodes)) nodes"))
                    .font(.caption.monospaced()).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 6)
    }
}

/// Time, mode, saving; the cost before and after; how many nodes moved; why it failed.
private struct CastAIPlanRow: View {
    let plan: CastAIPlan
    let scale: Double

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                Circle().fill(plan.planState.color).frame(width: 8, height: 8).accessibilityHidden(true)
                Text(verbatim: castAITime(plan.createdAt)).font(.subheadline)
                if let mode = plan.mode.label { CastAIModeChip(label: mode) }
                Spacer()
                Text(verbatim: plan.savingText).font(.caption.monospaced()).foregroundStyle(plan.savingColor)
            }
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 10) {
                    CastAIBeforeAfterBar(before: plan.beforeMonthly, after: plan.afterMonthly, scale: scale)
                    Text(verbatim: formatMoney(plan.beforeMonthly, currency: plan.currency) + " → " + formatMoney(plan.afterMonthly, currency: plan.currency))
                        .font(.caption2.monospaced()).foregroundStyle(.secondary).fixedSize()
                }
                if !plan.nodesText.isEmpty {
                    Text(verbatim: plan.nodesText).font(.caption2).foregroundStyle(.secondary)
                }
                if plan.planState == .failed {
                    Text(verbatim: plan.failureText).font(.caption2).foregroundStyle(.statusBad)
                }
            }
            .padding(.leading, 18)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(Text(verbatim: plan.planState.label))
    }
}

/// The consolidation's mode ("full", "empty nodes") as a small grey chip.
struct CastAIModeChip: View {
    let label: String

    var body: some View {
        Text(verbatim: label)
            .font(.caption2)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(Color(.tertiarySystemFill), in: RoundedRectangle(cornerRadius: 6))
    }
}
