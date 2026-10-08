import Foundation

/// The Kubernetes home's nodes, the way the overview handles a large Talos cluster
/// (NodeDensity, whose counts, groups, problem list and filters serve both): a kube node is
/// never `.unreachable`, Kubernetes only says ready or not. Same as Android's KubeNodeDensity.

public extension KubeNodeInfo {
    /// Not ready; else ready but cordoned or under pressure (`.attention`); else ready.
    var status: NodeStatus {
        if !ready { return .notReady }
        return cordoned || !pressure.isEmpty ? .attention : .ready
    }
}

extension KubeNodeInfo: NodeStatusProviding {
    public var isReady: Bool { ready }

    /// The name, or the internal or external address.
    public func matches(query: String) -> Bool {
        name.localizedCaseInsensitiveContains(query)
            || (internalIP?.localizedCaseInsensitiveContains(query) ?? false)
            || (externalIP?.localizedCaseInsensitiveContains(query) ?? false)
    }
}

/// The filters the Kubernetes nodes screen offers: no node is unreachable to Kubernetes.
public let kubeNodeFilters: [NodeFilter] = [.attention, .ready, .notReady]
