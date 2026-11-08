import Foundation

// Background alerts on KubeSpan tunnels (on by default, Talos clusters only, see Monitor.swift): the
// Go core's KubeSpanMeshHealth and the English wording (BackgroundMonitor rebuilds it in the user's
// language). Same rules as Android's KubeSpanIssues.kt.

/// The tunnels Go judged broken from every node's peers. `issues`: "<idA>|<idB>" → KubeSpanIssue
/// value. `partial`: a node could not be read, so a tunnel missing from `issues` may only be out of sight.
public struct KubeSpanMeshHealth: Decodable, Equatable, Sendable {
    public let context: String
    public let checked: Bool
    public let partial: Bool
    public let links: Int
    public let broken: Int
    public let issues: [String: String]

    public init(
        context: String = "", checked: Bool = false, partial: Bool = false, links: Int = 0, broken: Int = 0,
        issues: [String: String] = [:]
    ) {
        self.context = context
        self.checked = checked
        self.partial = partial
        self.links = links
        self.broken = broken
        self.issues = issues
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        checked = try c.field(.checked, false)
        partial = try c.field(.partial, false)
        links = try c.field(.links, 0)
        broken = try c.field(.broken, 0)
        issues = try c.field(.issues, [:])
    }

    private enum CodingKeys: String, CodingKey { case context, checked, partial, links, broken, issues }
}

/// A KubeSpan issue's stored value as Go writes it: "severity|reason|since|host-a|host-b", `since`
/// the unix seconds of the last handshake (0 = never).
public struct KubeSpanIssue: Equatable, Sendable {
    public let severity: String
    public let reason: String
    public let since: Int64
    public let hostA: String
    public let hostB: String

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 4, omittingEmptySubsequences: false).map(String.init)
        func part(_ i: Int) -> String { i < parts.count ? parts[i] : "" }
        severity = part(0)
        reason = part(1)
        since = Int64(part(2)) ?? 0
        hostA = part(3)
        hostB = part(4)
    }

    /// "node-a ↔ node-b".
    public var subject: String { "\(hostA) ↔ \(hostB)" }
}

/// The tunnels `health` says are broken; nil when nothing is known (no KubeSpan, or no node read:
/// never an all-clear). On a partial read the `known` ones stay until every node answers.
public func kubespanIssuesOf(_ health: KubeSpanMeshHealth, known: [String: String] = [:]) -> [String: String]? {
    guard health.checked else { return nil }
    return health.partial ? known.merging(health.issues) { _, new in new } : health.issues
}

/// The KubeSpan issues kept by the previous snapshot, when it is the same cluster and read them.
public func knownKubespanIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.kubespanWatched, previous.kubespanChecked else {
        return [:]
    }
    return previous.kubespanIssues
}

/// English title of a tunnel issue, or of its end.
func kubespanTitle(_ issue: KubeSpanIssue, problem: Bool) -> String {
    problem ? "Tunnel \(issue.subject) is down" : "Tunnel \(issue.subject) is back"
}

/// KubeSpan tunnels (see evaluateTrack), keyed "kubespan:<key>": Go's grace is over, so every issue
/// is critical and alerts at once. A cleared one is named from what the previous snapshot kept.
func evaluateKubespan(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert])
    -> IssueTrack
{
    let known = previous?.kubespanIssues ?? [:]
    return evaluateTrack(
        previous: previous?.kubespanTrack, current: current.kubespanTrack, comparable: comparable,
        severity: { _ in dataCritical },
        problem: { key, value in
            let issue = KubeSpanIssue(value: value)
            return Alert(key: "kubespan:\(key)", title: kubespanTitle(issue, problem: true), text: "", problem: true)
        },
        cleared: { key in
            let issue = KubeSpanIssue(value: known[key] ?? "")
            return Alert(key: "kubespan:\(key)", title: kubespanTitle(issue, problem: false), text: "", problem: false)
        },
        alerts: &alerts)
}
