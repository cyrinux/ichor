import Foundation

// Background alerts on Argo CD and Flux apps (opt-in, see Monitor.swift): which apps are worth a
// notification, and the English wording (BackgroundMonitor rebuilds it in the user's language).

/// Why a GitOps app alerts; the raw values are stored in the snapshot.
public enum GitOpsReason: String, Sendable, CaseIterable {
    /// Argo CD: the last sync operation failed (phase Failed or Error).
    case syncFailed
    /// Argo CD: health Degraded.
    case degraded
    /// Argo CD: health Missing.
    case missing
    /// Argo CD: a ComparisonError, InvalidSpecError or SyncError condition.
    case error
    /// Argo CD: out of sync while auto-sync is on (it should have synced).
    case outOfSync
    /// Flux: a Kustomization or HelmRelease that is failing.
    case notReady
}

/// A GitOps issue key ("argocd|namespace/name" or "flux|Kind namespace/name") split up.
public struct GitOpsSubject: Equatable, Sendable {
    /// "argocd" or "flux".
    public let tool: String
    /// The Flux kind (Kustomization, HelmRelease), "" for Argo CD.
    public let kind: String
    public let namespace: String
    public let name: String

    public init(key: String) {
        let parts = key.split(separator: "|", maxSplits: 1).map(String.init)
        tool = parts.first ?? ""
        var rest = parts.count > 1 ? parts[1] : key
        if tool == "flux", let space = rest.firstIndex(of: " ") {
            kind = String(rest[..<space])
            rest = String(rest[rest.index(after: space)...])
        } else {
            kind = ""
        }
        let path = rest.split(separator: "/", maxSplits: 1).map(String.init)
        namespace = path.count > 1 ? path[0] : ""
        name = path.last ?? rest
    }

    public var isFlux: Bool { tool == "flux" }
    /// "namespace/name".
    public var label: String { namespace.isEmpty ? name : "\(namespace)/\(name)" }
    /// What the notification names: the app, or "HelmRelease ingress-nginx" for Flux.
    public var title: String { kind.isEmpty ? name : "\(kind) \(name)" }
}

/// A GitOps issue's stored value, "severity:reason", split up.
public func gitopsSeverity(_ value: String) -> String {
    String(value.split(separator: ":", maxSplits: 1).first ?? Substring(dataWarning))
}

public func gitopsReason(_ value: String) -> GitOpsReason? {
    value.split(separator: ":", maxSplits: 1).last.flatMap { GitOpsReason(rawValue: String($0)) }
}

/// The Argo CD condition types that alert.
private let argoAlertingConditions: Set<String> = ["ComparisonError", "InvalidSpecError", "SyncError"]

/// The Argo CD and Flux apps worth a notification, keyed "argocd|namespace/name" and
/// "flux|Kind namespace/name", valued "severity:reason". Argo CD: a failed last sync, a Degraded or
/// Missing app is critical; out of sync while auto-sync is on, or a comparison, spec or sync error
/// condition, a warning. Progressing, Suspended, or out of sync with auto-sync off are not issues.
/// Flux: a failing app is critical; a suspended one never alerts, a reconciling one is no issue.
/// A tool not installed (or not read) adds nothing.
public func gitopsIssuesOf(argo: ArgoStatus?, flux: FluxStatus?) -> [String: String] {
    var out: [String: String] = [:]
    if let argo, argo.installed {
        for app in argo.apps {
            if let reason = argoAlertReason(app) {
                let severity = reason == .outOfSync || reason == .error ? dataWarning : dataCritical
                out["argocd|\(app.namespace)/\(app.name)"] = "\(severity):\(reason.rawValue)"
            }
        }
    }
    if let flux, flux.installed {
        for app in flux.apps where !app.suspended && app.level == .critical {
            out["flux|\(app.kind) \(app.namespace)/\(app.name)"] = "\(dataCritical):\(GitOpsReason.notReady.rawValue)"
        }
    }
    return out
}

/// gitopsIssuesOf for a check that read only part of the apps (same as Android): nil when neither
/// tool could be read; otherwise the issues of what was read, plus those `known` (the previous
/// snapshot's) had for the parts that were not: Argo CD when argo is nil, Flux when flux is nil,
/// HelmReleases when Flux could not list them (helmError). An unread part neither alerts nor clears.
public func gitopsIssuesWithGaps(argo: ArgoStatus?, flux: FluxStatus?, known: [String: String]) -> [String: String]? {
    if argo == nil && flux == nil { return nil }
    var unread: [String] = []
    if argo == nil { unread.append("argocd|") }
    if let flux {
        if !flux.helmError.isEmpty { unread.append("flux|HelmRelease ") }
    } else {
        unread.append("flux|")
    }
    var out = gitopsIssuesOf(argo: argo, flux: flux)
    for (key, value) in known where out[key] == nil && unread.contains(where: key.hasPrefix) {
        out[key] = value
    }
    return out
}

/// The worst reason an Argo CD app alerts for, nil when it does not.
private func argoAlertReason(_ app: ArgoApp) -> GitOpsReason? {
    if app.operation?.phase.failed == true { return .syncFailed }
    if app.health == .degraded { return .degraded }
    if app.health == .missing { return .missing }
    if app.conditions.contains(where: { argoAlertingConditions.contains($0.type) }) { return .error }
    if app.sync == .outOfSync && app.autoSync.enabled { return .outOfSync }
    return nil
}

/// English title of a GitOps problem ("Argo CD: grafana sync failed").
func gitopsProblemTitle(_ subject: GitOpsSubject, reason: GitOpsReason?) -> String {
    let name = subject.title
    switch reason {
    case .syncFailed: return "Argo CD: \(name) sync failed"
    case .degraded: return "Argo CD: \(name) is degraded"
    case .missing: return "Argo CD: \(name) has missing resources"
    case .error: return "Argo CD: \(name) has an error"
    case .outOfSync: return "Argo CD: \(name) is out of sync"
    case .notReady, nil: return subject.isFlux ? "Flux: \(name) is not ready" : "Argo CD: \(name) needs attention"
    }
}

/// English title of a GitOps issue that cleared.
func gitopsClearedTitle(_ subject: GitOpsSubject) -> String {
    subject.isFlux ? "Flux: \(subject.title) is ready again" : "Argo CD: \(subject.title) is synced and healthy again"
}

/// Argo CD and Flux issues (see evaluateTrack), keyed "gitops:tool|subject".
func evaluateGitOps(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert]) -> IssueTrack {
    evaluateTrack(
        previous: previous?.gitopsTrack, current: current.gitopsTrack, comparable: comparable, severity: gitopsSeverity,
        problem: { key, value in
            let subject = GitOpsSubject(key: key)
            return Alert(key: "gitops:\(key)", title: gitopsProblemTitle(subject, reason: gitopsReason(value)),
                         text: "\(subject.label) · \(gitopsSeverity(value))", problem: true)
        },
        cleared: { key in
            let subject = GitOpsSubject(key: key)
            return Alert(key: "gitops:\(key)", title: gitopsClearedTitle(subject), text: subject.label, problem: false)
        },
        alerts: &alerts)
}

/// The GitOps issues a partial read may carry over: the previous snapshot's, only when it is the
/// same cluster and that check watched and read them. Another cluster's issues must never leak in.
public func knownGitOpsIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.gitopsWatched, previous.gitopsChecked else { return [:] }
    return previous.gitopsIssues
}
