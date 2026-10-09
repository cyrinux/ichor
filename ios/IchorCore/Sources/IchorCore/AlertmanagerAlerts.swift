import Foundation

// Background alerts on the Alertmanager's alerts (opt-in, see Monitor.swift): which alerts are
// worth a notification, and the English wording (BackgroundMonitor rebuilds it in the user's
// language).

/// An Alertmanager issue's stored value, "severity|alertname|subject|summary", split up: what the
/// notification names, kept so a resolved alert can still be named once it is gone.
public struct AMIssue: Equatable, Sendable {
    /// dataCritical or dataWarning.
    public let severity: String
    public let alertname: String
    /// What it is about ("namespace/pod", the instance...), "" when nothing says.
    public let subject: String
    public let summary: String

    public init(severity: String, alertname: String, subject: String = "", summary: String = "") {
        self.severity = severity
        self.alertname = alertname
        self.subject = subject
        self.summary = summary
    }

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 3, omittingEmptySubsequences: false).map(String.init)
        severity = parts.first.flatMap { $0.isEmpty ? nil : $0 } ?? dataWarning
        alertname = parts.count > 1 ? parts[1] : ""
        subject = parts.count > 2 ? parts[2] : ""
        summary = parts.count > 3 ? parts[3] : ""
    }

    /// The stored form; the alert name loses any "|" so the parts still split.
    public var value: String {
        [severity, alertname.replacingOccurrences(of: "|", with: "/"), subject.replacingOccurrences(of: "|", with: "/"), summary]
            .joined(separator: "|")
    }

    /// "namespace/pod · critical", the severity alone without a subject.
    public func line(severity label: String) -> String { subject.isEmpty ? label : "\(subject) · \(label)" }
}

/// The alerts worth a notification, keyed by fingerprint: only those not suppressed (silenced or
/// inhibited), a critical one as critical, a warning or one without a known severity as a warning.
/// Info alerts (Watchdog and the like) never notify.
public func alertmanagerIssuesOf(_ alerts: AMAlerts) -> [String: String] {
    var out: [String: String] = [:]
    for alert in alerts.alerts where !alert.suppressed && !alert.fingerprint.isEmpty {
        let severity: String
        switch alert.severity {
        case .critical: severity = dataCritical
        case .warning, .other: severity = dataWarning
        case .info: continue
        }
        let summary = String(alert.summary.prefix(200))
        out[alert.fingerprint] = AMIssue(severity: severity, alertname: alert.alertname, subject: alert.subject, summary: summary).value
    }
    return out
}

/// alertmanagerIssuesOf for a check that may have read only part of the alerts: past 1000
/// (`truncated`), an alert `known` (the previous snapshot's) missing from the answer may still
/// fire, so it is kept rather than reported resolved. One in the answer is judged on what it says.
public func alertmanagerIssuesWithGaps(_ alerts: AMAlerts, known: [String: String]) -> [String: String] {
    var out = alertmanagerIssuesOf(alerts)
    guard alerts.truncated else { return out }
    let seen = Set(alerts.alerts.map(\.fingerprint))
    for (key, value) in known where out[key] == nil && !seen.contains(key) {
        out[key] = value
    }
    return out
}

/// The Alertmanager issues a truncated read may carry over: the previous snapshot's, only when it
/// is the same cluster and that check watched and read them. Another cluster's must never leak in.
public func knownAlertmanagerIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.amWatched, previous.amChecked else { return [:] }
    return previous.amIssues
}

/// English title of an alert firing.
func amProblemTitle(_ issue: AMIssue) -> String { "Alertmanager: \(issue.alertname)" }

/// English title of an alert resolved.
func amClearedTitle(_ issue: AMIssue) -> String { "Alertmanager: \(issue.alertname) resolved" }

/// Alertmanager alerts (see evaluateTrack), keyed "am:fingerprint". A resolved alert is named
/// from what the previous snapshot kept of it.
func evaluateAlertmanager(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert]) -> IssueTrack {
    let known = previous?.amIssues ?? [:]
    return evaluateTrack(
        previous: previous?.amTrack, current: current.amTrack, comparable: comparable, severity: { AMIssue(value: $0).severity },
        problem: { key, value in
            let issue = AMIssue(value: value)
            var text = issue.line(severity: issue.severity)
            if !issue.summary.isEmpty { text += "\n\(issue.summary)" }
            return Alert(key: "am:\(key)", title: amProblemTitle(issue), text: text, problem: true)
        },
        cleared: { key in
            let issue = AMIssue(value: known[key] ?? "")
            return Alert(key: "am:\(key)", title: amClearedTitle(issue), text: issue.subject, problem: false)
        },
        alerts: &alerts)
}
