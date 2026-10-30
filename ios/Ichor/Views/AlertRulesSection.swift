import SwiftUI
import IchorCore

/// The "Rules" group of the background alerts: quiet hours, the minimum severity and the digest
/// by default, and a "Per cluster" row for the overrides (AlertRulesClusterSheet), as Android's
/// AlertRulesGroup. Read at each check: nothing to confirm, nothing destructive.
struct AlertRulesSection: View {
    @Environment(AppModel.self) private var model
    /// The background alerts are on (MonitoringSection's switch).
    @AppStorage(BackgroundMonitor.alertsKey) private var alertsOn = false
    @State private var rules = AlertRulesStore.load()
    @State private var perCluster = false

    var body: some View {
        let rule = rules.default
        let quiet = rule.quietHours.flatMap { $0.isOff ? nil : $0 }
        Section {
            RuleToggle(
                title: String(localized: "Quiet hours"), hint: String(localized: "No notification between these times"),
                isOn: quiet != nil
            ) { on in
                update { $0.quietHours = on ? QuietHours.night : nil }
            }
            if let quiet {
                QuietHoursRows(quiet: quiet) { next in update { $0.quietHours = next } }
            }
            RuleSegments(
                title: String(localized: "Minimum severity"), labels: alertRuleSeverities.map(alertSeverityLabel),
                selected: alertRuleSeverities.firstIndex(of: rule.minSeverity ?? alertSeverityAll) ?? 0
            ) { index in
                let severity = alertRuleSeverities[index]
                update { $0.minSeverity = severity == alertSeverityAll ? nil : severity }
            }
            RuleToggle(
                title: String(localized: "Digest"),
                hint: String(localized: "One notification per cluster and check with its counts, instead of one per alert"),
                isOn: rule.digest == true
            ) { on in
                update { $0.digest = on ? true : nil }
            }
            Button {
                perCluster = true
            } label: {
                let count = rules.clusters.count
                let title =
                    count == 0 ? String(localized: "Per cluster") : String(localized: "Per cluster: \(count) clusters")
                Text(title)
            }
        } header: {
            Text("Rules")
        } footer: {
            Text("Applied at each check: an alert raised during quiet hours is not posted again once they end.")
        }
        .disabled(!alertsOn)
        .sheet(isPresented: $perCluster) {
            AlertRulesClusterSheet(
                contexts: model.summary?.contexts ?? [], labels: model.labels, rules: rules
            ) { save($0) }
        }
    }

    private func update(_ change: (inout AlertRule) -> Void) {
        var rule = rules.default
        change(&rule)
        save(rules.withDefault(rule))
    }

    private func save(_ next: AlertRules) {
        AlertRulesStore.save(next)
        rules = next
    }
}
