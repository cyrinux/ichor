import Foundation

/// A screen a share link opens (the JSON of the Go core's BuildShareLink and ParseShareLink):
/// `cluster` is the `ContextSummary.clusterID` of the cluster, so the link works on any phone
/// holding a talosconfig for it. A link only navigates, it never acts.
public struct ShareTarget: Codable, Equatable, Hashable, Sendable {
    public enum Target: String, Codable, Sendable {
        case cluster, etcd, health, argoCD = "argocd", flux, node, workloads
        case argoApp = "argo-app", fluxApp = "flux-app", workload, pod, cronJob = "cronjob"
        /// The data services screen, on the tab `kind` names (a catalog id); the cluster checkup;
        /// the Alertmanager alerts.
        case data, checkup, alerts
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
        cluster = try c.field(.cluster, "")
        target = try c.decode(Target.self, forKey: .target)
        host = try c.field(.host, "")
        addr = try c.field(.addr, "")
        tab = try c.field(.tab, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
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

    /// The data services screen on `kind`'s tab; nil: the first one.
    public static func data(_ kind: DataServiceKind?) -> ShareTarget {
        ShareTarget(target: .data, kind: kind?.catalogID ?? "")
    }

    /// The data services tab a `data` link opens; nil for the first one (a system this app does not know).
    public var dataServiceKind: DataServiceKind? {
        target == .data ? DataServiceKind(rawValue: kind) : nil
    }

    /// The screen a background alert is about (see evaluate for its keys), a node's hostname read
    /// from `snapshot`; nil when there is none: the Talos certificate alert opens the renewal.
    public static func forAlert(key: String, snapshot: ClusterSnapshot) -> ShareTarget? {
        let parts = key.split(separator: ":", maxSplits: 1).map(String.init)
        let subject = parts.count > 1 ? parts[1] : ""
        switch parts.first ?? "" {
        case "node":
            guard !subject.isEmpty else { return nil }
            return .node(address: subject, hostname: snapshot.nodes[subject]?.hostname ?? "", tab: "")
        case "etcd": return .screen(.etcd)
        case "cert": return snapshot.kube ? .screen(.cluster) : nil
        case "data": return .data(DataServiceKind(alertKey: subject))
        case "gitops":
            let app = GitOpsSubject(key: subject)
            switch app.tool {
            case "argocd": return .argoApp(namespace: app.namespace, name: app.name)
            case "flux": return .fluxApp(kind: app.kind, namespace: app.namespace, name: app.name)
            default: return nil
            }
        case "checkup": return .screen(.checkup)
        case "am": return .screen(.alerts)
        case "unreachable": return .screen(.cluster)
        default: return nil
        }
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

extension DataServiceKind {
    /// The system of a data alert key ("system|label", see dataIssuesOf); nil for one it does not name.
    public init?(alertKey: String) {
        let system = alertKey.split(separator: "|", maxSplits: 1).first.map(String.init) ?? ""
        switch system {
        case "cnpg": self = .cnpg
        case "percona": self = .percona
        case "certmanager": self = .certManager
        case "ceph": self = .ceph
        default: self.init(rawValue: system)
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
