import SwiftUI
import IchorCore

/// The Policies screen's texts with arguments: passed as Strings, so each key is the `%@` one
/// the catalog holds (an Int would look up `%lld`).
enum PolicyText {
    static func reportsRead(_ a: String) -> String { String(localized: "\(a) reports read") }
    static func reportsTruncated(_ a: String) -> String { String(localized: "Reports truncated: the counts cover the first \(a) reports") }
    static func reportsUnavailable(_ a: String) -> String { String(localized: "Reports unavailable: \(a)") }
    static func readError(_ a: String) -> String { String(localized: "Not everything could be read: \(a)") }
    static func controllerReady(_ a: String, _ b: String, _ c: String) -> String { String(localized: "\(a): \(b) of \(c) ready") }
    static func failCount(_ a: String) -> String { String(localized: "\(a) failing") }
    static func warnCount(_ a: String) -> String { String(localized: "\(a) warnings") }
    static func passCount(_ a: String) -> String { String(localized: "\(a) passing") }
    static func ruleOf(_ a: String) -> String { String(localized: "Rule \(a)") }
    static func failingShown(_ a: String, _ b: String) -> String { String(localized: "The latest \(a) of \(b)") }
    static func switchGitOps(_ a: String) -> String { String(localized: "\(a) applies this policy: its next sync puts the previous mode back.") }
    static func switchFailed(_ a: String, _ b: String) -> String { String(localized: "Could not switch \(a): \(b)") }
    static func failingCapped(_ a: String, _ b: String) -> String { String(localized: "Gatekeeper keeps the first \(a) of \(b) violations") }
}

extension PolicyAction {
    /// Enforce stands out (it refuses requests), Audit and Warn are warnings, Mixed sits between.
    var color: Color {
        switch self {
        case .enforce: .statusBad
        case .audit, .warn: .statusWarn
        case .mixed: .purple
        case .unknown: .secondary
        }
    }

    var label: Text {
        switch self {
        case .enforce: Text("Enforce")
        case .audit: Text("Audit")
        case .mixed: Text("Mixed")
        case .warn: Text("Warn")
        case .unknown: Text(verbatim: "?")
        }
    }

    /// What the mode does to a request that breaks a rule.
    var meaning: Text? {
        switch self {
        case .enforce: Text("A request that breaks a rule is refused")
        case .audit: Text("A request that breaks a rule is let through and reported")
        case .mixed: Text("Some rules refuse a request that breaks them, others only report it")
        case .warn: Text("A request that breaks it is let through with a warning")
        case .unknown: nil
        }
    }
}

/// The policy's mode as a small tinted label, read out as "Mode: Enforce".
struct PolicyActionBadge: View {
    let action: PolicyAction

    var body: some View {
        action.label
            .font(.caption2.weight(.bold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(action.color)
            .background(action.color.opacity(0.15), in: RoundedRectangle(cornerRadius: 5, style: .continuous))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("Mode") + Text(verbatim: ": ") + action.label)
    }
}

/// "Not ready" in the error colour, read out with its reason.
struct PolicyNotReadyBadge: View {
    let reason: String

    var body: some View {
        Text("Not ready")
            .font(.caption2.weight(.bold))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(Color.statusBad)
            .background(Color.statusBad.opacity(0.15), in: RoundedRectangle(cornerRadius: 5, style: .continuous))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(reason.isEmpty ? Text("Not ready") : Text("Not ready") + Text(verbatim: ": \(reason)"))
    }
}

/// The failing, warning and passing counts, each only when it is not zero.
struct PolicyCounts: View {
    let fail: Int
    let warn: Int
    let pass: Int

    var body: some View {
        if fail > 0 || warn > 0 || pass > 0 {
            HStack(spacing: 6) {
                if fail > 0 { InfoChip(text: PolicyText.failCount(String(fail)), color: .statusBad) }
                if warn > 0 { InfoChip(text: PolicyText.warnCount(String(warn)), color: .statusWarn) }
                if pass > 0 { InfoChip(text: PolicyText.passCount(String(pass)), color: .statusOK) }
            }
        }
    }
}

extension KyvernoPolicy {
    /// Namespace (or cluster-wide), a constraint's kind, category and severity, as the row's second line.
    var scopeText: Text {
        let scope = clusterWide ? Text("Cluster-wide") : Text(verbatim: namespace)
        let rest = [kind, category, severity].filter { !$0.isEmpty }
        return rest.isEmpty ? scope : scope + Text(verbatim: "  ·  " + rest.joined(separator: "  ·  "))
    }
}
