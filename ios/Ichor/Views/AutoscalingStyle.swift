import SwiftUI
import IchorCore

/// The Autoscaling screen's texts with arguments: passed as Strings, so each key is the `%@`
/// one the catalog holds (an Int would look up `%lld`).
enum AutoscaleText {
    static func desired(_ a: String) -> String { String(localized: "wants %@ replicas") }
    static func lastScaled(_ a: String) -> String { String(localized: "last scaled %@") }
    static func managedBy(_ a: String, _ b: String) -> String { String(localized: "managed by %1$@ (%2$@)") }
    static func metricValues(_ a: String, _ b: String) -> String { String(localized: "%1$@ of %2$@") }
    static func pausedAt(_ a: String) -> String { String(localized: "paused at %@ replicas") }
    static func fallbackReplicas(_ a: String) -> String { String(localized: "falling back to %@ replicas") }
    static func cpuChange(_ a: String) -> String { String(localized: "CPU: %@") }
    static func memoryChange(_ a: String) -> String { String(localized: "Memory: %@") }
    static func bounds(_ a: String, _ b: String, _ c: String, _ d: String) -> String { String(localized: "bounds %1$@–%2$@ CPU, %3$@–%4$@ memory") }
    static func poolUsage(_ a: String, _ b: String) -> String { String(localized: "cpu %1$@ of %2$@") }
    static func memoryUsage(_ a: String, _ b: String) -> String { String(localized: "memory %1$@ of %2$@") }
    static func poolNodes(_ a: String) -> String { String(localized: "%@ nodes") }
    static func poolClaims(_ a: String) -> String { String(localized: "%@ claims") }
    static func capacityTypes(_ a: String) -> String { String(localized: "Capacity types: %@") }
    static func consolidateAfter(_ a: String) -> String { String(localized: "consolidate after %@") }
    static func budgetNodes(_ a: String) -> String { String(localized: "%@ nodes") }
    static func notInstalled(_ a: String) -> String { String(localized: "%@ is not installed on this cluster") }
    static func groupError(_ a: String) -> String { String(localized: "This group could not be read: %@") }
    static func openWorkload(_ a: String) -> String { String(localized: "Open %@") }
    static func condition(_ a: String, _ b: String) -> String { String(localized: "%1$@: %2$@") }
}

extension AutoscaleState {
    /// Problems first, in their colour: not ready and at max are errors, limited, fallback
    /// and paused are warnings, ok is fine.
    var color: Color {
        switch self {
        case .notReady, .atMax: .statusBad
        case .limited, .fallback: .statusWarn
        case .paused: .purple
        case .ok: .statusOK
        case .unknown: .secondary
        }
    }

    var label: Text {
        switch self {
        case .notReady: Text("not ready")
        case .atMax: Text("at max")
        case .limited: Text("limited")
        case .fallback: Text("fallback")
        case .paused: Text("paused")
        case .ok: Text("ok")
        case .unknown: Text(verbatim: "?")
        }
    }
}

/// The state as a small tinted label, read out as "State: at max".
struct AutoscaleStateBadge: View {
    let state: AutoscaleState

    var body: some View {
        state.label
            .font(.caption2.weight(.bold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(state.color)
            .background(state.color.opacity(0.15), in: RoundedRectangle(cornerRadius: 5, style: .continuous))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("State") + Text(verbatim: ": ") + state.label)
    }
}

/// A small tinted fact, read out as it reads.
struct AutoscaleChip: View {
    let text: String
    let color: Color

    var body: some View {
        Text(verbatim: text)
            .font(.caption2.weight(.bold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: RoundedRectangle(cornerRadius: 5, style: .continuous))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(verbatim: text))
    }
}

/// A NodeClaim's badges: pending, drifted, disrupting — the states worth a look.
struct NodeClaimBadges: View {
    let claim: NodeClaimRow

    var body: some View {
        HStack(spacing: 6) {
            if claim.pending { AutoscaleChip(text: "pending", color: .statusBad) }
            if claim.drifted { AutoscaleChip(text: "drifted", color: .statusWarn) }
            if claim.disrupting { AutoscaleChip(text: "disrupting", color: .statusWarn) }
            if !claim.ready && !claim.pending { AutoscaleChip(text: "not ready", color: .statusBad) }
        }
    }
}

extension AutoscaleTarget {
    /// The resource browser's type of a well-known target kind, to open the object; nil for
    /// another (shown as text).
    public var resource: KubeAPIResource? {
        switch kind {
        case "Pod": return KubeAPIResource(resource: "pods", kind: kind)
        case "Service": return KubeAPIResource(resource: "services", kind: kind)
        case "Deployment": return KubeAPIResource(group: "apps", resource: "deployments", kind: kind)
        case "StatefulSet": return KubeAPIResource(group: "apps", resource: "statefulsets", kind: kind)
        case "DaemonSet": return KubeAPIResource(group: "apps", resource: "daemonsets", kind: kind)
        case "ReplicaSet": return KubeAPIResource(group: "apps", resource: "replicasets", kind: kind)
        case "Job": return KubeAPIResource(group: "batch", resource: "jobs", kind: kind)
        case "CronJob": return KubeAPIResource(group: "batch", resource: "cronjobs", kind: kind)
        default: return nil
        }
    }
}
