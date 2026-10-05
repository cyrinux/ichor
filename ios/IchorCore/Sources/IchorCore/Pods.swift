import Foundation

// Mirrors go/ichorgo/kube_pods.go.

public struct KubePodList: Decodable, Equatable, Sendable {
    public let pods: [KubePod]

    public init(pods: [KubePod] = []) { self.pods = pods }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pods = try c.decodeIfPresent([KubePod].self, forKey: .pods) ?? []
    }

    private enum CodingKeys: String, CodingKey { case pods }
}

/// A Kubernetes pod with the status `kubectl get pods` shows (KubePods).
public struct KubePod: Codable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Running, Pending, CrashLoopBackOff, Init:Error, Terminating...
    public let status: String
    /// Running with every container ready, or completed.
    public let healthy: Bool
    public let ready: Int
    public let containers: Int
    public let restarts: Int
    public let node: String
    /// "ReplicaSet/web-5d8f", empty when none.
    public let owner: String
    /// Unix ms.
    public let created: Int64
    public let images: [String]
    /// The pod's containers (not the init ones), in spec order.
    public let containerNames: [String]
    /// How a container last stopped, e.g. "OOMKilled (exit 137)"; "" when none did.
    public let lastTermination: String

    public var id: String { "\(namespace)/\(name)" }

    /// Pending, initializing or terminating: not broken, not done either.
    public var transitional: Bool {
        status == "Pending" || status == "Terminating" || (status.hasPrefix("Init:") && status.contains("/"))
    }

    public init(namespace: String, name: String, status: String = "", healthy: Bool = false, ready: Int = 0, containers: Int = 0,
                restarts: Int = 0, node: String = "", owner: String = "", created: Int64 = 0, images: [String] = [],
                containerNames: [String] = [], lastTermination: String = "") {
        self.namespace = namespace
        self.name = name
        self.status = status
        self.healthy = healthy
        self.ready = ready
        self.containers = containers
        self.restarts = restarts
        self.node = node
        self.owner = owner
        self.created = created
        self.images = images
        self.containerNames = containerNames
        self.lastTermination = lastTermination
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.decode(String.self, forKey: .namespace)
        name = try c.decode(String.self, forKey: .name)
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        healthy = try c.decodeIfPresent(Bool.self, forKey: .healthy) ?? false
        ready = try c.decodeIfPresent(Int.self, forKey: .ready) ?? 0
        containers = try c.decodeIfPresent(Int.self, forKey: .containers) ?? 0
        restarts = try c.decodeIfPresent(Int.self, forKey: .restarts) ?? 0
        node = try c.decodeIfPresent(String.self, forKey: .node) ?? ""
        owner = try c.decodeIfPresent(String.self, forKey: .owner) ?? ""
        created = try c.decodeIfPresent(Int64.self, forKey: .created) ?? 0
        images = try c.decodeIfPresent([String].self, forKey: .images) ?? []
        containerNames = try c.decodeIfPresent([String].self, forKey: .containerNames) ?? []
        lastTermination = try c.decodeIfPresent(String.self, forKey: .lastTermination) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, status, healthy, ready, containers, restarts, node, owner, created, images, containerNames, lastTermination
    }
}

/// Namespaces that have pods, sorted.
public func podNamespaces(_ pods: [KubePod]) -> [String] {
    Array(Set(pods.map(\.namespace))).sorted()
}

/// Pods of namespace (all when nil) whose name, status, node, owner or, when searchImages,
/// image contains query (case-insensitive). sorted: unhealthy ones first, then by namespace
/// and name; else in the order loaded (a list still incomplete).
public func filterPods(_ pods: [KubePod], namespace: String?, query: String, sorted: Bool = true,
                       searchImages: Bool = true) -> [KubePod] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    let contains = { (text: String) in text.range(of: needle, options: .caseInsensitive) != nil }
    let matching = pods.filter { p in
        (namespace == nil || p.namespace == namespace) &&
            (needle.isEmpty || [p.name, p.status, p.node, p.owner].contains(where: contains) ||
                searchImages && p.images.contains(where: contains))
    }
    guard sorted else { return matching }
    return matching
        .sorted { a, b in
            if a.healthy != b.healthy { return !a.healthy }
            if a.namespace != b.namespace { return a.namespace < b.namespace }
            return a.name < b.name
        }
}
