import Foundation

/// How a node hub row reaches the node: through its Talos API (fails while the node does not
/// answer) or through the Kubernetes API server (works while it is down).
public enum NodeTransport: Sendable {
    case talos, kube
}

/// The rows of the node hub, as the transport table sees them (the app's NodeHubScreen, the
/// tabs and the power rows).
public enum NodeHubEntry: String, CaseIterable, Sendable {
    // Status: the tabs, then the Kubernetes pods.
    case services, resources, live, processes, containers, cgroups, kubePods
    // Inspect.
    case kernelLog, events, network, storage, images, resourceBrowser, hardware
    // Troubleshoot.
    case debugShell, netTools, capture
    // Operate.
    case machineConfig, upgrade, reset, maintenance, drain, cordon, taintsLabels
    // Power: reboot and shut down.
    case power

    /// Listed one by one: a new row has to say how it reaches the node.
    public var transport: NodeTransport {
        switch self {
        case .kubePods, .maintenance, .drain, .cordon, .taintsLabels:
            .kube
        case .services, .resources, .live, .processes, .containers, .cgroups, .kernelLog, .events, .network,
            .storage, .images, .resourceBrowser, .hardware, .debugShell, .netTools, .capture, .machineConfig,
            .upgrade, .reset, .power:
            .talos
        }
    }
}

/// Whether a node answers its Talos API, as the overview (or a call that just failed) says:
/// `errorKind` network, tls, auth or other, `error` the core's words, `lastSeen` when it last
/// answered (epoch ms). Unknown is nil, never an unreachable value: a row is only dimmed on
/// evidence.
public struct NodeReachability: Equatable, Hashable, Sendable {
    public let reachable: Bool
    public let errorKind: String?
    public let error: String?
    public let lastSeen: Int64?

    public init(reachable: Bool, errorKind: String? = nil, error: String? = nil, lastSeen: Int64? = nil) {
        self.reachable = reachable
        self.errorKind = errorKind
        self.error = error
        self.lastSeen = lastSeen
    }

    /// The core's prefix for a node it could not dial (friendlyError in go/ichorgo/errors.go).
    static let unreachablePrefix = "unreachable: "

    /// An unreachable node from the message of a call to it that failed because it could not
    /// be dialled; nil for any other failure (refused, too old, slow).
    public static func from(failure message: String) -> NodeReachability? {
        guard message.hasPrefix(unreachablePrefix) else { return nil }
        return NodeReachability(reachable: false, errorKind: "network", error: message)
    }

    /// What the hub knows of a node: a `live` failure wins over what the last overview said
    /// (`known`), as the node may have gone down since; the overview's own "down" (with when
    /// it last answered) is kept.
    public static func merge(known: NodeReachability?, live: NodeReachability?) -> NodeReachability? {
        guard let live else { return known }
        guard let known, !known.reachable else { return live }
        return known
    }
}

extension Optional where Wrapped == NodeReachability {
    /// True only when the node is known not to answer; unknown counts as reachable.
    public var isDown: Bool { self?.reachable == false }
}

extension ClusterOverview {
    /// `node` as this overview saw it; nil when it does not list it.
    public func reachability(of node: String) -> NodeReachability? {
        nodes.first { $0.node == node }.map {
            NodeReachability(reachable: $0.reachable, errorKind: $0.errorKind, error: $0.error, lastSeen: $0.lastSeen)
        }
    }
}

/// How long the end of the window may go without a check of a node before the uptime strip
/// says so: past the usual spacing of the background checks.
public let historyTailGapMillis: Int64 = 60 * 60 * 1000

extension HistoryQueryResult {
    /// How long `uptime`'s node has gone unchecked at the end of this window: from its last
    /// interval to `to`, when over historyTailGapMillis; nil otherwise, and for a node never
    /// observed. Its uptime % only covers the time before.
    public func uncheckedTail(_ uptime: HistoryNodeUptime) -> Int64? {
        guard let last = uptime.intervals.map(\.to).max() else { return nil }
        let tail = to - last
        return tail > historyTailGapMillis ? tail : nil
    }
}
