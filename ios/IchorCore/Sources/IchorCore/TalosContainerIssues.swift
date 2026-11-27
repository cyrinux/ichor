import Foundation

// Background alerts on Talos containers (Talos 1.15, on by default, Talos clusters only, see
// Monitor.swift): the Go core's TalosContainersHealth and the English wording (BackgroundMonitor
// rebuilds it in the user's language). Same rules as Android's TalosContainerIssues.kt.

/// Every node's Talos containers in trouble. `issues`: "<node>|<name>" → TalosContainerIssue
/// value; `unread`: the nodes that did not answer; `checked`: at least one did.
public struct TalosContainersHealth: Decodable, Equatable, Sendable {
    public let context: String
    public let checked: Bool
    public let unread: [String]
    public let issues: [String: String]

    public init(context: String = "", checked: Bool = false, unread: [String] = [], issues: [String: String] = [:]) {
        self.context = context
        self.checked = checked
        self.unread = unread
        self.issues = issues
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        checked = try c.field(.checked, false)
        unread = try c.field(.unread, [])
        issues = try c.field(.issues, [:])
    }

    private enum CodingKeys: String, CodingKey { case context, checked, unread, issues }
}

/// A Talos container issue's stored value as Go writes it:
/// "severity|state|restartCount|exitCode|hostname|name". Never its error text nor its last log:
/// they are workload output, not for a notification.
public struct TalosContainerIssue: Equatable, Sendable {
    public let severity: String
    public let state: String
    public let restartCount: Int64
    public let exitCode: Int64
    public let hostname: String
    public let name: String

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 5, omittingEmptySubsequences: false).map(String.init)
        func part(_ i: Int) -> String { i < parts.count ? parts[i] : "" }
        severity = part(0)
        state = part(1)
        restartCount = Int64(part(2)) ?? 0
        exitCode = Int64(part(3)) ?? 0
        hostname = part(4)
        name = part(5)
    }

    /// Talos gave up on it: its restart policy does not start it again.
    public var failed: Bool { state == "failed" }
}

/// The Talos containers `health` says are in trouble; nil when no node answered (never an
/// all-clear). A node that did not answer keeps its `known` issues.
public func talosContainerIssuesOf(
    _ health: TalosContainersHealth, known: [String: String] = [:]
) -> [String: String]? {
    guard health.checked else { return nil }
    let unread = Set(health.unread)
    let kept = known.filter { key, _ in unread.contains(String(key.split(separator: "|").first ?? "")) }
    return kept.merging(health.issues) { _, new in new }
}

/// The Talos container issues kept by the previous snapshot, when it is the same cluster and read them.
public func knownTalosContainerIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.talosContainerWatched, previous.talosContainerChecked
    else {
        return [:]
    }
    return previous.talosContainerIssues
}

/// English title of a Talos container issue, or of its end.
func talosContainerTitle(_ issue: TalosContainerIssue, problem: Bool) -> String {
    problem ? "\(issue.name) on \(issue.hostname) is failing" : "\(issue.name) on \(issue.hostname) is running again"
}

/// English text of a Talos container issue: its state and counts only.
func talosContainerText(_ issue: TalosContainerIssue) -> String {
    issue.failed
        ? "Failed with exit code \(issue.exitCode): Talos does not start it again."
        : "Restarted \(issue.restartCount) times, last exit code \(issue.exitCode)."
}

/// Talos containers (see evaluateTrack), keyed "taloscontainer:<node>|<name>": a failed one alerts
/// at once, one that keeps restarting on its second check in a row. A cleared one is named from
/// what the previous snapshot kept.
func evaluateTalosContainers(
    previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert]
) -> IssueTrack {
    let known = previous?.talosContainerIssues ?? [:]
    return evaluateTrack(
        previous: previous?.talosContainerTrack, current: current.talosContainerTrack, comparable: comparable,
        severity: { TalosContainerIssue(value: $0).severity == dataCritical ? dataCritical : dataWarning },
        problem: { key, value in
            let issue = TalosContainerIssue(value: value)
            return Alert(
                key: "taloscontainer:\(key)", title: talosContainerTitle(issue, problem: true),
                text: talosContainerText(issue), problem: true)
        },
        cleared: { key in
            let issue = TalosContainerIssue(value: known[key] ?? "")
            return Alert(
                key: "taloscontainer:\(key)", title: talosContainerTitle(issue, problem: false),
                text: "Talos containers", problem: false)
        },
        alerts: &alerts)
}
