import Foundation

// Background alerts on the Talos hypervisor's virtual machines (on by default, Talos clusters only,
// see Monitor.swift): the problems the Go core judged in ClusterVirtualMachines and the English
// wording (BackgroundMonitor rebuilds it in the user's language). Same rules as Android's
// TalosVmIssues.kt.

/// A Talos VM that should run and does not (go/ichorgo/talos_vm_problems.go): `kind` vmDown,
/// vmError or vmDiskFailed; `detail` the node's own words, clipped.
public struct TalosVMProblem: Decodable, Equatable, Hashable, Sendable {
    public let node: String
    public let vm: String
    public let kind: String
    public let detail: String

    public init(node: String, vm: String, kind: String, detail: String = "") {
        self.node = node
        self.vm = vm
        self.kind = kind
        self.detail = detail
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        vm = try c.field(.vm, "")
        kind = try c.field(.kind, "")
        detail = try c.field(.detail, "")
    }

    private enum CodingKeys: String, CodingKey { case node, vm, kind, detail }
}

/// One node of TalosVMProblemReport: its problems, or `error` when it could not be read.
public struct TalosVMProblemNode: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    public let problems: [TalosVMProblem]
    /// Section (vms, specs, disks, libraries) to its error, for the sections that failed.
    public let errors: [String: String]
    public let error: String

    public init(
        node: String, hostname: String = "", problems: [TalosVMProblem] = [], errors: [String: String] = [:],
        error: String = ""
    ) {
        self.node = node
        self.hostname = hostname
        self.problems = problems
        self.errors = errors
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        problems = try c.field(.problems, [])
        errors = try c.field(.errors, [:])
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, problems, errors, error }
}

/// What the monitor reads of ClusterVirtualMachines: every node's problems, not its VMs.
public struct TalosVMProblemReport: Decodable, Equatable, Sendable {
    public let context: String
    public let nodes: [TalosVMProblemNode]

    public init(context: String = "", nodes: [TalosVMProblemNode] = []) {
        self.context = context
        self.nodes = nodes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        nodes = try c.field(.nodes, [])
    }

    private enum CodingKeys: String, CodingKey { case context, nodes }
}

/// A Talos VM issue's stored value: "severity|kind|hostname|vm|detail".
public struct TalosVMIssue: Equatable, Sendable {
    public static let down = "vmDown"
    public static let error = "vmError"
    public static let diskFailed = "vmDiskFailed"

    public let severity: String
    public let kind: String
    public let hostname: String
    public let vm: String
    public let detail: String

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 4, omittingEmptySubsequences: false).map(String.init)
        func part(_ i: Int) -> String { i < parts.count ? parts[i] : "" }
        severity = part(0)
        kind = part(1)
        hostname = part(2)
        vm = part(3)
        detail = part(4)
    }

    /// The stored value of a problem seen on hostname; "|" in the text becomes "/".
    public static func value(_ problem: TalosVMProblem, hostname: String) -> String {
        [dataCritical, problem.kind, hostname, problem.vm, problem.detail]
            .map { $0.replacingOccurrences(of: "|", with: "/") }
            .joined(separator: "|")
    }
}

/// The issue key of a problem: "<node>|vm|<name>", stable across checks.
public func talosVMIssueKey(_ problem: TalosVMProblem) -> String { "\(problem.node)|vm|\(problem.vm)" }

/// The Talos VM issues of report. A node that did not answer, or whose VM statuses could not be
/// read, keeps its known issues: unreadable, not resolved. A node older than Talos 1.15 has none.
public func talosVMIssuesOf(_ report: TalosVMProblemReport, known: [String: String] = [:]) -> [String: String] {
    var out: [String: String] = [:]
    for node in report.nodes {
        if !node.error.isEmpty || node.errors["vms"] != nil {
            for (key, value) in known where key.split(separator: "|").first.map(String.init) == node.node {
                out[key] = value
            }
            continue
        }
        let host = node.hostname.isEmpty ? node.node : node.hostname
        for problem in node.problems {
            out[talosVMIssueKey(problem)] = TalosVMIssue.value(problem, hostname: host)
        }
    }
    return out
}

/// The Talos VM issues kept by the previous snapshot, when it is the same cluster and read them.
public func knownTalosVMIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.vmWatched, previous.vmChecked else { return [:] }
    return previous.vmIssues
}

/// English title of a VM problem, or of its end.
func talosVMTitle(_ issue: TalosVMIssue, problem: Bool) -> String {
    guard problem else { return "\(issue.vm) on \(issue.hostname) is back to normal" }
    switch issue.kind {
    case TalosVMIssue.diskFailed: return "A disk of \(issue.vm) failed on \(issue.hostname)"
    case TalosVMIssue.error: return "\(issue.vm) failed on \(issue.hostname)"
    default: return "\(issue.vm) is down on \(issue.hostname)"
    }
}

/// Talos VMs (see evaluateTrack), keyed "vm:<key>": a VM that should run and does not is critical
/// and alerts at once. A cleared one is named from what the previous snapshot kept.
func evaluateTalosVMs(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert])
    -> IssueTrack
{
    let known = previous?.vmIssues ?? [:]
    return evaluateTrack(
        previous: previous?.vmTrack, current: current.vmTrack, comparable: comparable,
        severity: { _ in dataCritical },
        problem: { key, value in
            let issue = TalosVMIssue(value: value)
            return Alert(key: "vm:\(key)", title: talosVMTitle(issue, problem: true), text: issue.detail, problem: true)
        },
        cleared: { key in
            let issue = TalosVMIssue(value: known[key] ?? "")
            return Alert(key: "vm:\(key)", title: talosVMTitle(issue, problem: false), text: "", problem: false)
        },
        alerts: &alerts)
}
