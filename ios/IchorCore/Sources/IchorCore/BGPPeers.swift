import Foundation

// A node's own BGP sessions (Talos 1.14+ native BGP, go/ichorgo/network_bgp.go) and the background
// alert on them (on by default, Talos clusters only, see Monitor.swift): the sessions the Go core
// judged down in ClusterBGPPeers and the English wording (BackgroundMonitor rebuilds it in the
// user's language). Same rules as Android's BgpIssues.kt.

/// One BGP session of a node. `peer` is the neighbour's address, or "link/<name>" for an
/// unnumbered one; `since` is RFC 3339, set only while established.
public struct BGPPeer: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let instance: String
    public let peer: String
    public let peerAsn: Int64
    public let localAsn: Int64
    /// IDLE, CONNECT, ACTIVE, OPEN_SENT, OPEN_CONFIRM, ESTABLISHED or UNKNOWN.
    public let state: String
    public let established: Bool
    public let since: String
    public let received: Int64
    public let advertised: Int64
    /// Talos 1.15+: graceful restart was negotiated.
    public let gracefulRestart: Bool
    /// Talos 1.15+: the peer is restarting and its routes are kept meanwhile (not down).
    public let peerRestarting: Bool

    public var id: String { "\(instance)/\(peer)" }

    /// Neither up nor kept by graceful restart.
    public var isDown: Bool { !established && !peerRestarting }

    /// When the session came up; nil when it is not up or the time is unknown.
    public var sinceDate: Date? {
        since.isEmpty ? nil : ISO8601DateFormatter().date(from: since)
    }

    public init(
        instance: String = "", peer: String = "", peerAsn: Int64 = 0, localAsn: Int64 = 0, state: String = "",
        established: Bool = false, since: String = "", received: Int64 = 0, advertised: Int64 = 0,
        gracefulRestart: Bool = false, peerRestarting: Bool = false
    ) {
        self.instance = instance
        self.peer = peer
        self.peerAsn = peerAsn
        self.localAsn = localAsn
        self.state = state
        self.established = established
        self.since = since
        self.received = received
        self.advertised = advertised
        self.gracefulRestart = gracefulRestart
        self.peerRestarting = peerRestarting
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        instance = try c.field(.instance, "")
        peer = try c.field(.peer, "")
        peerAsn = try c.field(.peerAsn, 0)
        localAsn = try c.field(.localAsn, 0)
        state = try c.field(.state, "")
        established = try c.field(.established, false)
        since = try c.field(.since, "")
        received = try c.field(.received, 0)
        advertised = try c.field(.advertised, 0)
        gracefulRestart = try c.field(.gracefulRestart, false)
        peerRestarting = try c.field(.peerRestarting, false)
    }

    private enum CodingKeys: String, CodingKey {
        case instance, peer, peerAsn, localAsn, state, established, since, received, advertised
        case gracefulRestart, peerRestarting
    }
}

/// One node of BGPPeerReport: its sessions, or `error` when it could not be read.
public struct BGPNodePeers: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    public let supported: Bool
    public let peers: [BGPPeer]
    public let error: String

    public init(
        node: String, hostname: String = "", supported: Bool = false, peers: [BGPPeer] = [], error: String = ""
    ) {
        self.node = node
        self.hostname = hostname
        self.supported = supported
        self.peers = peers
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        supported = try c.field(.supported, false)
        peers = try c.field(.peers, [])
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, supported, peers, error }
}

/// A session down, as Go judged it: not established, and its peer not restarting.
public struct BGPDown: Decodable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    public let instance: String
    public let peer: String
    public let state: String

    public init(node: String, hostname: String = "", instance: String = "", peer: String = "", state: String = "") {
        self.node = node
        self.hostname = hostname
        self.instance = instance
        self.peer = peer
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        instance = try c.field(.instance, "")
        peer = try c.field(.peer, "")
        state = try c.field(.state, "")
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, instance, peer, state }
}

/// What the monitor reads of ClusterBGPPeers: every node's sessions and those down.
public struct BGPPeerReport: Decodable, Equatable, Sendable {
    public let context: String
    public let nodes: [BGPNodePeers]
    public let down: [BGPDown]

    public init(context: String = "", nodes: [BGPNodePeers] = [], down: [BGPDown] = []) {
        self.context = context
        self.nodes = nodes
        self.down = down
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        nodes = try c.field(.nodes, [])
        down = try c.field(.down, [])
    }

    private enum CodingKeys: String, CodingKey { case context, nodes, down }
}

/// A BGP issue's stored value: "severity|hostname|instance|peer|state".
public struct BGPIssue: Equatable, Sendable {
    public let severity: String
    public let hostname: String
    public let instance: String
    public let peer: String
    public let state: String

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 4, omittingEmptySubsequences: false).map(String.init)
        func part(_ i: Int) -> String { i < parts.count ? parts[i] : "" }
        severity = part(0)
        hostname = part(1)
        instance = part(2)
        peer = part(3)
        state = part(4)
    }

    /// The stored value of a session down; "|" in the text becomes "/".
    public static func value(_ down: BGPDown) -> String {
        [dataWarning, down.hostname.isEmpty ? down.node : down.hostname, down.instance, down.peer, down.state]
            .map { $0.replacingOccurrences(of: "|", with: "/") }
            .joined(separator: "|")
    }
}

/// The issue key of a session down: "<node>|<instance>|<peer>", stable across checks.
public func bgpIssueKey(_ down: BGPDown) -> String {
    let instance = down.instance.replacingOccurrences(of: "|", with: "/")
    let peer = down.peer.replacingOccurrences(of: "|", with: "/")
    return "\(down.node)|\(instance)|\(peer)"
}

extension ClusterSnapshot {
    var bgpTrack: IssueTrack {
        get { IssueTrack(watched: bgpWatched, checked: bgpChecked, issues: bgpIssues, pending: bgpPending) }
        set {
            (bgpWatched, bgpChecked, bgpIssues, bgpPending) = (
                newValue.watched, newValue.checked, newValue.issues, newValue.pending
            )
        }
    }
}

/// The BGP issues of report. A node that did not answer keeps its known issues: unreadable, not
/// resolved. A node without BGP (or older than Talos 1.14) has none.
public func bgpIssuesOf(_ report: BGPPeerReport, known: [String: String] = [:]) -> [String: String] {
    var out: [String: String] = [:]
    for node in report.nodes where !node.error.isEmpty {
        for (key, value) in known where key.split(separator: "|").first.map(String.init) == node.node {
            out[key] = value
        }
    }
    for down in report.down {
        out[bgpIssueKey(down)] = BGPIssue.value(down)
    }
    return out
}

/// The BGP issues kept by the previous snapshot, when it is the same cluster and read them.
public func knownBGPIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.bgpWatched, previous.bgpChecked else { return [:] }
    return previous.bgpIssues
}

/// English title of a session down, or back up.
func bgpTitle(_ issue: BGPIssue, problem: Bool) -> String {
    guard problem else { return "BGP session to \(issue.peer) is up again on \(issue.hostname)" }
    return "BGP session to \(issue.peer) is down on \(issue.hostname)"
}

/// BGP sessions (see evaluateTrack), keyed "bgp:<key>": a session down is a warning, notified only
/// when still down at the next check, so a node's own reboot or a flap never notifies.
func evaluateBGP(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert])
    -> IssueTrack
{
    let known = previous?.bgpIssues ?? [:]
    return evaluateTrack(
        previous: previous?.bgpTrack, current: current.bgpTrack, comparable: comparable,
        severity: { _ in dataWarning },
        problem: { key, value in
            let issue = BGPIssue(value: value)
            return Alert(key: "bgp:\(key)", title: bgpTitle(issue, problem: true), text: issue.state, problem: true)
        },
        cleared: { key in
            let issue = BGPIssue(value: known[key] ?? "")
            return Alert(key: "bgp:\(key)", title: bgpTitle(issue, problem: false), text: "", problem: false)
        },
        alerts: &alerts)
}
