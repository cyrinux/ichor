import SwiftUI
import IchorCore

extension AMSeverity {
    var color: Color {
        switch self {
        case .critical: .red
        case .warning: attentionColor
        case .info: .blue
        case .other: .secondary
        }
    }

    var label: String {
        switch self {
        case .critical: String(localized: "Critical")
        case .warning: String(localized: "Warning")
        case .info: String(localized: "Info")
        case .other: String(localized: "Other")
        }
    }

    var symbol: String {
        switch self {
        case .critical: "exclamationmark.octagon.fill"
        case .warning: "exclamationmark.triangle.fill"
        case .info: "info.circle.fill"
        case .other: "bell.fill"
        }
    }
}

extension AMAlert {
    /// "Firing", "Silenced", "Inhibited" (or both), "Unprocessed".
    var stateLabel: String {
        switch state {
        case .active: return String(localized: "Firing")
        case .unprocessed: return String(localized: "Unprocessed")
        case .suppressed:
            if silenced && inhibited { return String(localized: "Silenced and inhibited") }
            return inhibited ? String(localized: "Inhibited") : String(localized: "Silenced")
        }
    }
}

/// The home's Alerts card: the Alertmanager's firing alerts by severity and those suppressed,
/// opening the alerts screen. Only shown once an Alertmanager is found (and the role may use the
/// Kubernetes API); a calm line when nothing fires.
struct AlertsSection: View {
    let state: LoadState<AMAlerts>

    var body: some View {
        switch state {
        case .loading:
            Section {
                VStack(alignment: .leading, spacing: 10) {
                    header(firing: 0)
                    Text(verbatim: "Critical Warning Info").font(.caption)
                }
                .redacted(reason: .placeholder)
            }
        case .failed(let message):
            Section {
                NavigationLink(value: Route.alerts) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Alerts").font(.headline)
                        Text("Could not read: \(message)").font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            }
        case .loaded(let alerts, _, _):
            Section {
                NavigationLink(value: Route.alerts) { content(alerts) }
            }
        }
    }

    private func header(firing: Int) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "bell.badge")
                .frame(width: 28, height: 28)
                .background(.quaternary, in: RoundedRectangle(cornerRadius: 8))
                .accessibilityHidden(true)
            Text("Alerts").font(.headline)
            Spacer()
            Text("\(firing) firing").font(.subheadline).foregroundStyle(.secondary).monospacedDigit()
        }
    }

    private func content(_ alerts: AMAlerts) -> some View {
        let counts = alerts.counts
        return VStack(alignment: .leading, spacing: 10) {
            header(firing: counts.firing)
            if counts.firing == 0 && counts.suppressed == 0 {
                Text("No alerts firing").font(.caption).foregroundStyle(.secondary)
            } else {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 12) { countItems(counts) }
                    VStack(alignment: .leading, spacing: 2) { countItems(counts) }
                }
                .font(.caption)
                .monospacedDigit()
                // The worst firing groups, at most two.
                ForEach(alerts.groups.filter { $0.active > 0 && $0.severity.rank <= AMSeverity.warning.rank }.prefix(2)) { group in
                    HStack(spacing: 8) {
                        Image(systemName: group.severity.symbol).foregroundStyle(group.severity.color).font(.caption)
                            .accessibilityHidden(true)
                        Text(verbatim: group.active > 1 ? "\(group.alertname) ×\(group.active)" : group.alertname)
                            .font(.caption.weight(.semibold))
                            .lineLimit(1)
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder private func countItems(_ counts: AMCounts) -> some View {
        ForEach([AMSeverity.critical, .warning, .info], id: \.self) { severity in
            HStack(spacing: 4) {
                Circle().fill(severity.color).frame(width: 7, height: 7).accessibilityHidden(true)
                Text(verbatim: "\(counts.count(severity)) \(severity.label.lowercased())")
            }
        }
        if counts.other > 0 {
            HStack(spacing: 4) {
                Circle().fill(AMSeverity.other.color).frame(width: 7, height: 7).accessibilityHidden(true)
                Text(verbatim: "\(counts.other) \(AMSeverity.other.label.lowercased())")
            }
        }
        if counts.suppressed > 0 {
            HStack(spacing: 4) {
                Image(systemName: "bell.slash").foregroundStyle(.secondary).accessibilityHidden(true)
                Text("\(counts.suppressed) suppressed")
            }
        }
    }
}
