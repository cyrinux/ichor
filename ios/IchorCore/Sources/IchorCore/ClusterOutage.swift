import Foundation

/// Why no node answered, from the core's per-node error kinds.
public enum OutageCause: Sendable {
    /// Dial failures or timeouts: most likely the phone is off the cluster's network (VPN, LAN).
    case network
    /// The cluster answered but refused this talosconfig (certificate, CA or role).
    case credentials
    /// Anything else, or a mix of causes.
    case other
}

/// No node answered: the overview shows this once instead of a list of unreachable nodes.
public struct ClusterOutage: Equatable, Sendable {
    public let cause: OutageCause
    /// How many nodes were tried.
    public let nodes: Int
    /// The distinct errors, in node order: usually the same one from every node.
    public let errors: [String]
}

/// While no node answers, the overview tries again this often (and at once when the network changes).
public let unreachableRetrySeconds = 15

extension ClusterOverview {
    /// Nil as long as one node answered (or there is none to try).
    public var outage: ClusterOutage? {
        guard !nodes.isEmpty, !nodes.contains(where: \.reachable) else { return nil }
        let kinds = Set(nodes.map(\.errorKind))
        let cause: OutageCause
        if kinds == ["network"] {
            cause = .network
        } else if kinds.allSatisfy({ $0 == "tls" || $0 == "auth" }) {
            cause = .credentials
        } else {
            cause = .other
        }
        var errors: [String] = []
        for case let error? in nodes.map(\.error) where !error.isEmpty && !errors.contains(error) {
            errors.append(error)
        }
        return ClusterOutage(cause: cause, nodes: nodes.count, errors: errors)
    }
}
