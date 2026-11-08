import Foundation

// What the home shows from the last background check before the cluster answers (a seed): the
// nodes and their health, nothing else. Never kept as last known. Same rules as Android.

/// How old a background check may be to seed the home: as long as a last known state is kept.
public let monitorSeedMaxAge: TimeInterval = 24 * 60 * 60

extension ClusterSnapshot {
    /// The overview this check saw: each node, reachable and ready or not, with why it is down.
    public func overviewSeed() -> ClusterOverview {
        let nodes = nodes.map { node, state in
            let reachable = state.health != .unreachable
            return NodeOverview(
                node: node, hostname: state.hostname, reachable: reachable,
                error: !reachable && !state.reason.isEmpty ? state.reason : nil,
                ready: state.health == .ready
            )
        }
        return ClusterOverview(
            context: context,
            nodes: nodes.sorted {
                ($0.hostname.isEmpty ? $0.node : $0.hostname) < ($1.hostname.isEmpty ? $1.node : $1.hostname)
            })
    }

    /// The Kubernetes nodes this check of a kubeconfig cluster saw.
    public func kubeNodesSeed() -> KubeNodesOverview {
        let nodes = nodes.values.map { state in
            KubeNodeInfo(
                name: state.hostname, ready: state.health == .ready,
                pressure: state.reason.components(separatedBy: "; ").filter { !$0.isEmpty }
            )
        }
        return KubeNodesOverview(nodes: nodes.sorted { $0.name < $1.name })
    }

    /// This check, when it may seed the home: recent, of the right kind (`kube`) and with nodes;
    /// never in screenshot mode (`masked`: the check keeps the real names).
    public func seedable(kube: Bool, masked: Bool, now: Date = Date()) -> Bool {
        !masked && self.kube == kube && !nodes.isEmpty && now.timeIntervalSince(takenAt) <= monitorSeedMaxAge
    }
}
