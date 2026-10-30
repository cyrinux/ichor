import SwiftUI
import IchorCore

/// Per-cluster overrides of the alert rules, as Android's AlertRulesClusterSheet: for each
/// stored cluster (the demo while it is stored), quiet hours, the minimum severity and the
/// digest, each "As default" unless set. A removed cluster's overrides go with it
/// (AlertRulesStore.keep).
struct AlertRulesClusterSheet: View {
    let contexts: [ContextSummary]
    let labels: ClusterLabels
    let onChange: (AlertRules) -> Void
    @State private var rules: AlertRules
    @State private var open: String?
    @Environment(\.dismiss) private var dismiss

    init(
        contexts: [ContextSummary], labels: ClusterLabels, rules: AlertRules,
        onChange: @escaping (AlertRules) -> Void
    ) {
        self.contexts = contexts
        self.labels = labels
        self.onChange = onChange
        _rules = State(initialValue: rules)
    }

    /// The stored clusters with a fingerprint, once each (several contexts can be one cluster).
    private var clusters: [ContextSummary] {
        var seen = Set<String>()
        return contexts.filter { !$0.fingerprint.isEmpty && seen.insert($0.fingerprint).inserted }
    }

    var body: some View {
        NavigationStack {
            Form {
                if clusters.isEmpty {
                    Text("No cluster stored yet.").foregroundStyle(.secondary)
                }
                ForEach(clusters, id: \.fingerprint) { context in
                    let fingerprint = context.fingerprint
                    let rule = rules.clusterRule(fingerprint)
                    Section {
                        DisclosureGroup(
                            isExpanded: Binding(
                                get: { open == fingerprint }, set: { open = $0 ? fingerprint : nil })
                        ) {
                            ClusterRuleEditor(rule: rule) { set(fingerprint, $0) }
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: labels.of(context)).lineLimit(1)
                                Text(rule.isEmpty ? "As default" : "Own rules")
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
            .navigationTitle(Text("Rules per cluster"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
    }

    private func set(_ fingerprint: String, _ rule: AlertRule) {
        rules = rules.withCluster(fingerprint, rule)
        onChange(rules)
    }
}

/// A cluster's three overrides, each with an "As default" choice.
private struct ClusterRuleEditor: View {
    let rule: AlertRule
    let onChange: (AlertRule) -> Void

    /// As the default, none, or a window of its own.
    private var quietChoice: Int {
        guard let quiet = rule.quietHours else { return 0 }
        return quiet.isOff ? 1 : 2
    }

    var body: some View {
        let asDefault = String(localized: "As default")
        let off = String(localized: "Off")
        RuleSegments(
            title: String(localized: "Quiet hours"),
            labels: [asDefault, off, String(localized: "alertRules.custom", defaultValue: "Custom")],
            selected: quietChoice
        ) { index in
            var next = rule
            switch index {
            case 0: next.quietHours = nil
            case 1: next.quietHours = QuietHours.off
            default: next.quietHours = rule.quietHours.flatMap { $0.isOff ? nil : $0 } ?? QuietHours.night
            }
            onChange(next)
        }
        if let quiet = rule.quietHours, !quiet.isOff {
            QuietHoursRows(quiet: quiet) { window in
                var next = rule
                next.quietHours = window
                onChange(next)
            }
        }
        RuleSegments(
            title: String(localized: "Minimum severity"),
            labels: [asDefault] + alertRuleSeverities.map(alertSeverityLabel),
            selected: rule.minSeverity.flatMap { alertRuleSeverities.firstIndex(of: $0) }.map { $0 + 1 } ?? 0
        ) { index in
            var next = rule
            next.minSeverity = index == 0 ? nil : alertRuleSeverities[index - 1]
            onChange(next)
        }
        RuleSegments(
            title: String(localized: "Digest"),
            labels: [asDefault, off, String(localized: "alertRules.on", defaultValue: "On")],
            selected: rule.digest.map { $0 ? 2 : 1 } ?? 0
        ) { index in
            var next = rule
            next.digest = index == 0 ? nil : index == 2
            onChange(next)
        }
    }
}
