import SwiftUI
import IchorCore

/// The pools with a target down, most down first; each down target opens its pod or Service,
/// the pool its ServiceMonitor, PodMonitor, Probe or ScrapeConfig.
struct PromTargetsList: View {
    let targets: PromTargets
    let source: PromSource

    var body: some View {
        let pools = targets.troubledPools
        List {
            Section {
                Text(verbatim: PromMonitoringText.targetsDown("\(targets.down)", "\(targets.total)"))
                    .font(.headline)
                    .foregroundStyle(targets.down > 0 ? Color.statusWarn : Color.primary)
                if targets.unknown > 0 {
                    Text(verbatim: PromMonitoringText.notScrapedYet("\(targets.unknown)")).font(.footnote).foregroundStyle(.secondary)
                }
                if targets.truncated {
                    Text(verbatim: PromMonitoringText.targetsTruncated).font(.footnote).foregroundStyle(.secondary)
                }
                Text("Source: \(source.label)").font(.footnote).foregroundStyle(.secondary)
            }
            if pools.isEmpty {
                Section {
                    Label { Text(verbatim: PromMonitoringText.allTargetsUp) } icon: {
                        Image(systemName: "checkmark.circle").foregroundStyle(Color.statusOK)
                    }
                }
            }
            ForEach(pools) { pool in
                Section {
                    if let monitor = pool.monitorLink {
                        KubeObjectLinkRow(link: monitor) { KubeObjectLinkLabel(link: monitor) }
                    }
                    ForEach(pool.targets) { target in
                        KubeObjectLinkRow(link: target.link(in: pool)) { PromTargetRow(target: target) }
                    }
                } header: {
                    HStack {
                        Text(verbatim: pool.label).lineLimit(1).truncationMode(.middle)
                        Spacer()
                        Text(verbatim: PromMonitoringText.poolDown("\(pool.down)", "\(pool.up + pool.down + pool.unknown)"))
                            .monospacedDigit()
                    }
                }
            }
        }
    }
}

/// One down target: where Prometheus scraped, why it failed, when, and what it scrapes.
private struct PromTargetRow: View {
    let target: PromDownTarget

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: target.instance.isEmpty ? target.scrapeUrl : target.instance)
                .font(.subheadline.monospaced().weight(.semibold))
                .lineLimit(1)
                .truncationMode(.middle)
            if !target.lastError.isEmpty {
                Text(verbatim: target.lastError).font(.caption.monospaced()).foregroundStyle(Color.statusBad).lineLimit(4)
            }
            Text(verbatim: promAgo(target.lastScrape).map(PromMonitoringText.lastScrape) ?? PromMonitoringText.neverScraped)
                .font(.caption)
                .foregroundStyle(.secondary)
            if !target.pod.isEmpty {
                Text(verbatim: PromMonitoringText.podLine("\(target.namespace)/\(target.pod)")).font(.caption).foregroundStyle(.secondary)
            } else if !target.service.isEmpty {
                Text(verbatim: PromMonitoringText.serviceLine("\(target.namespace)/\(target.service)")).font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }
}

/// The rule groups, troubled first (open), with their PrometheusRule and each rule's state.
struct PromRulesList: View {
    let rules: PromRules

    var body: some View {
        let counts = rules.counts
        List {
            Section {
                Text(verbatim: PromMonitoringText.ruleCounts("\(counts.groups)", "\(counts.rules)")).font(.headline)
                Text(verbatim: PromMonitoringText.ruleStates("\(counts.firing)", "\(counts.pending)", "\(counts.errors)"))
                    .font(.callout)
                    .foregroundStyle(counts.errors > 0 ? Color.statusBad : Color.secondary)
                if rules.truncated {
                    Text(verbatim: PromMonitoringText.rulesTruncated).font(.footnote).foregroundStyle(.secondary)
                }
            }
            if rules.groups.isEmpty {
                Section { Text(verbatim: PromMonitoringText.noRules).foregroundStyle(.secondary) }
            }
            ForEach(rules.groups) { group in
                PromRuleGroupSection(group: group)
            }
        }
    }
}

private struct PromRuleGroupSection: View {
    let group: PromRuleGroup
    @State private var expanded: Bool

    init(group: PromRuleGroup) {
        self.group = group
        _expanded = State(initialValue: group.inTrouble)
    }

    var body: some View {
        Section {
            DisclosureGroup(isExpanded: $expanded) {
                if let link = group.ruleLink {
                    KubeObjectLinkRow(link: link) { KubeObjectLinkLabel(link: link) }
                } else if !group.file.isEmpty {
                    Text(verbatim: group.file).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(2).truncationMode(.middle)
                }
                ForEach(Array(group.rules.enumerated()), id: \.offset) { _, rule in
                    PromRuleRow(rule: rule)
                }
            } label: {
                HStack(spacing: 8) {
                    Text(verbatim: group.name).font(.subheadline.monospaced().weight(.semibold)).lineLimit(2).truncationMode(.middle)
                    Spacer(minLength: 4)
                    if group.errors > 0 { PromCountChip(count: group.errors, color: Color.statusBad, label: PromMonitoringText.errorsLabel) }
                    if group.firing > 0 { PromCountChip(count: group.firing, color: Color.statusWarn, label: String(localized: "Firing")) }
                    if group.pending > 0 { PromCountChip(count: group.pending, color: .secondary, label: String(localized: "Pending")) }
                }
            }
        }
    }
}

private struct PromCountChip: View {
    let count: Int
    let color: Color
    let label: String

    var body: some View {
        Text(verbatim: "\(count)")
            .font(.caption.weight(.semibold).monospacedDigit())
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: Capsule())
            .accessibilityLabel(Text(verbatim: "\(label): \(count)"))
    }
}

/// One rule: its type, its state (alerting rules), its health and its last error.
private struct PromRuleRow: View {
    let rule: PromRule

    private var stateText: String? {
        guard rule.type == .alerting else { return nil }
        switch rule.state {
        case .firing: return String(localized: "Firing")
        case .pending: return String(localized: "Pending")
        case .inactive: return PromMonitoringText.inactive
        }
    }

    private var stateColor: Color? { rule.state == .firing ? Color.statusWarn : nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: rule.name).font(.callout.monospaced()).lineLimit(2).truncationMode(.middle)
            HStack(spacing: 6) {
                InfoChip(text: rule.type == .alerting ? PromMonitoringText.alerting : PromMonitoringText.recording)
                if let stateText { InfoChip(text: stateText, color: stateColor) }
                if rule.health == .err { InfoChip(text: PromMonitoringText.healthErr, color: Color.statusBad) }
                if rule.alerts > 0 { InfoChip(text: PromMonitoringText.activeAlerts("\(rule.alerts)")) }
            }
            if !rule.lastError.isEmpty {
                Text(verbatim: rule.lastError).font(.caption.monospaced()).foregroundStyle(Color.statusBad).lineLimit(6)
            }
        }
        .padding(.vertical, 2)
    }
}

/// The operator's Prometheus and Alertmanager objects (each opens in the browser), and its monitors.
struct PromOperatorList: View {
    let status: PromOperatorStatus

    var body: some View {
        List {
            if !status.error.isEmpty {
                Section { Text(verbatim: CheckupText.checkupSectionError(status.error)).font(.footnote).foregroundStyle(Color.statusBad) }
            }
            Section {
                ForEach(status.prometheuses) { server in
                    KubeObjectLinkRow(link: PromOperatorStatus.link(prometheus: server)) { PromServerRow(server: server) }
                }
                if status.prometheuses.isEmpty { Text(verbatim: PromMonitoringText.noObjects).foregroundStyle(.secondary) }
            } header: {
                Text(verbatim: "Prometheus")
            }
            Section {
                ForEach(status.alertmanagers) { server in
                    KubeObjectLinkRow(link: PromOperatorStatus.link(alertmanager: server)) { PromServerRow(server: server) }
                }
                if status.alertmanagers.isEmpty { Text(verbatim: PromMonitoringText.noObjects).foregroundStyle(.secondary) }
            } header: {
                Text(verbatim: "Alertmanager")
            }
            Section {
                count("ServiceMonitors", status.serviceMonitors)
                count("PodMonitors", status.podMonitors)
                count("PrometheusRules", status.prometheusRules)
                count("Probes", status.probes)
            } header: {
                Text(verbatim: PromMonitoringText.monitors)
            }
        }
    }

    private func count(_ kind: String, _ value: Int) -> some View {
        LabeledContent {
            Text(verbatim: "\(value)").monospacedDigit()
        } label: {
            Text(verbatim: kind)
        }
    }
}

extension PromOperatorHealth {
    var label: String {
        switch self {
        case .ok: String(localized: "Healthy")
        case .warning: String(localized: "Degraded")
        case .critical: PromMonitoringText.healthCritical
        }
    }

    var color: Color {
        switch self {
        case .ok: Color.statusOK
        case .warning: Color.statusWarn
        case .critical: Color.statusBad
        }
    }
}

/// A Prometheus or Alertmanager: health, replicas available of desired, version, conditions.
private struct PromServerRow: View {
    let server: PromOperatorServer

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: server.id).font(.subheadline.monospaced().weight(.semibold)).lineLimit(1).truncationMode(.middle)
                Spacer()
                StatusPill(label: server.health.label, color: server.health.color)
            }
            HStack(spacing: 6) {
                Text(verbatim: PromMonitoringText.replicasAvailable("\(server.available)", "\(server.desired)"))
                    .font(.caption)
                    .foregroundStyle(server.available < server.desired ? Color.statusWarn : Color.secondary)
                if server.shards > 1 {
                    Text(verbatim: PromMonitoringText.shards("\(server.shards)")).font(.caption).foregroundStyle(.secondary)
                }
                if !server.version.isEmpty { InfoChip(text: server.version, monospaced: true) }
                if server.paused { InfoChip(text: PromMonitoringText.paused, color: Color.statusWarn) }
            }
            ForEach(Array(server.conditions.enumerated()), id: \.offset) { _, condition in
                Text(verbatim: conditionLine(condition))
                    .font(.caption.monospaced())
                    .foregroundStyle(condition.healthy ? Color.secondary : Color.statusWarn)
                    .lineLimit(3)
            }
        }
        .padding(.vertical, 2)
    }

    /// "Available: Degraded (SomePodsNotReady) 1/2".
    private func conditionLine(_ c: PromOperatorCondition) -> String {
        var line = "\(c.type): \(c.status)"
        if !c.reason.isEmpty { line += " (\(c.reason))" }
        if !c.message.isEmpty { line += " \(c.message)" }
        return line
    }
}
