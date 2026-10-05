import Foundation

/// A screen a share link opens (the JSON of the Go core's BuildShareLink and ParseShareLink):
/// `cluster` is the `ContextSummary.clusterID` of the cluster, so the link works on any phone
/// holding a talosconfig for it. A link only navigates, it never acts.
public struct ShareTarget: Codable, Equatable, Hashable, Sendable {
    public enum Target: String, Codable, Sendable {
        case cluster, etcd, health, argoCD = "argocd", flux, node, workloads
        case argoApp = "argo-app", fluxApp = "flux-app", workload, pod, cronJob = "cronjob"
    }

    public var cluster: String
    public var target: Target
    public var host: String
    public var addr: String
    public var tab: String
    public var kind: String
    public var namespace: String
    public var name: String

    public init(cluster: String = "", target: Target, host: String = "", addr: String = "", tab: String = "",
                kind: String = "", namespace: String = "", name: String = "") {
        self.cluster = cluster
        self.target = target
        self.host = host
        self.addr = addr
        self.tab = tab
        self.kind = kind
        self.namespace = namespace
        self.name = name
    }

    private enum CodingKeys: String, CodingKey { case cluster, target, host, addr, tab, kind, namespace = "ns", name }

    // Go leaves out the fields a target does not take.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cluster = try c.decodeIfPresent(String.self, forKey: .cluster) ?? ""
        target = try c.decode(Target.self, forKey: .target)
        host = try c.decodeIfPresent(String.self, forKey: .host) ?? ""
        addr = try c.decodeIfPresent(String.self, forKey: .addr) ?? ""
        tab = try c.decodeIfPresent(String.self, forKey: .tab) ?? ""
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        namespace = try c.decodeIfPresent(String.self, forKey: .namespace) ?? ""
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
    }

    /// The JSON for BuildShareLink, without empty fields.
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        if !cluster.isEmpty { try c.encode(cluster, forKey: .cluster) }
        try c.encode(target, forKey: .target)
        for (key, value) in [(CodingKeys.host, host), (.addr, addr), (.tab, tab), (.kind, kind), (.namespace, namespace), (.name, name)]
        where !value.isEmpty {
            try c.encode(value, forKey: key)
        }
    }

    /// The node screen's tabs as links name them; the Android-only Kubernetes pods tab opens Services here.
    public static let nodeTabs = ["services", "resources", "live", "processes", "pods", "cgroups"]

    public static func screen(_ target: Target) -> ShareTarget { ShareTarget(target: target) }

    public static func node(address: String, hostname: String, tab: String) -> ShareTarget {
        ShareTarget(target: .node, host: hostname, addr: address, tab: nodeTabs.contains(tab) ? tab : "")
    }

    public static func kubernetes(tab: KubeFocus.Tab) -> ShareTarget {
        ShareTarget(target: .workloads, tab: tab.rawValue)
    }

    public static func argoApp(namespace: String, name: String) -> ShareTarget {
        ShareTarget(target: .argoApp, namespace: namespace, name: name)
    }

    public static func fluxApp(kind: String, namespace: String, name: String) -> ShareTarget {
        ShareTarget(target: .fluxApp, kind: kind, namespace: namespace, name: name)
    }

    public static func workload(kind: String, namespace: String, name: String) -> ShareTarget {
        ShareTarget(target: .workload, kind: kind, namespace: namespace, name: name)
    }

    public static func pod(namespace: String, name: String) -> ShareTarget {
        ShareTarget(target: .pod, namespace: namespace, name: name)
    }

    public static func cronJob(namespace: String, name: String) -> ShareTarget {
        ShareTarget(target: .cronJob, namespace: namespace, name: name)
    }

    /// What the Kubernetes screen opens for the link; nil for a target not on that screen.
    public var kubeFocus: KubeFocus? {
        switch target {
        case .workloads: KubeFocus(tab: KubeFocus.Tab(rawValue: tab) ?? .workloads)
        case .workload: KubeFocus(tab: .workloads, id: "\(kind)/\(namespace)/\(name)", namespace: namespace, name: name)
        case .pod: KubeFocus(tab: .pods, id: "\(namespace)/\(name)", namespace: namespace, name: name)
        case .cronJob: KubeFocus(tab: .cronJobs, id: "\(namespace)/\(name)", namespace: namespace, name: name)
        default: nil
        }
    }
}

/// The Kubernetes screen opened on `tab`, with the item whose row id is `id` shown: the list
/// scoped to `namespace` and searched for `name`.
public struct KubeFocus: Hashable, Sendable {
    public enum Tab: String, Hashable, Sendable { case workloads, pods, cronJobs = "cronjobs" }

    public let tab: Tab
    public let id: String
    public let namespace: String
    public let name: String

    public init(tab: Tab, id: String = "", namespace: String = "", name: String = "") {
        self.tab = tab
        self.id = id
        self.namespace = namespace
        self.name = name
    }
}

extension Array where Element == ContextSummary {
    /// The context to open a link of `clusterID` with: the active one when it is of that
    /// cluster (its role is kept), else the first of it; nil when the phone has no such cluster.
    public func context(forCluster clusterID: String, active: String?) -> ContextSummary? {
        guard !clusterID.isEmpty else { return nil }
        let ofCluster = filter { $0.clusterID == clusterID }
        return ofCluster.first { $0.name == active } ?? ofCluster.first
    }
}
