import Foundation

// Clusters added from a kubeconfig, without Talos (Go kube_import.go, kube_nodes.go).

/// The home of a cluster added from a kubeconfig: its nodes as the Kubernetes API sees them
/// (Go KubeNodes), since there is no Talos overview to read.
public struct KubeNodesOverview: Decodable, Equatable, Sendable {
    /// The API server's version, "" when /version could not be read.
    public let serverVersion: String
    public let nodes: [KubeNodeInfo]
    /// The credentials may not list nodes (e.g. a namespaced ServiceAccount).
    public let forbidden: Bool

    public init(serverVersion: String = "", nodes: [KubeNodeInfo] = [], forbidden: Bool = false) {
        self.serverVersion = serverVersion
        self.nodes = nodes
        self.forbidden = forbidden
    }

    private enum CodingKeys: String, CodingKey { case serverVersion, nodes, forbidden }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        serverVersion = try c.field(.serverVersion, "")
        nodes = try c.field(.nodes, [])
        forbidden = try c.field(.forbidden, false)
    }

    public var readyCount: Int { nodes.filter(\.ready).count }
}

/// A node for the Kubernetes home. CPU in cores, memory in bytes.
public struct KubeNodeInfo: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let roles: [String]
    public let ready: Bool
    public let cordoned: Bool
    public let internalIP: String?
    public let externalIP: String?
    public let kubelet: String?
    public let osImage: String?
    public let kernel: String?
    public let runtime: String?
    public let arch: String?
    /// The autoscaler pool the node came from; poolKind is "karpenter", "eks", "gke-class", "gke" or "aks".
    public let pool: String?
    public let poolKind: String?
    /// The cloud machine type (node.kubernetes.io/instance-type).
    public let instanceType: String?
    /// "spot", "on-demand" or "reserved" when the cloud labels say which.
    public let capacity: String?
    public let cpu: Double
    public let memory: Double
    public let podLimit: Int
    /// The problem conditions that are on (MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable).
    public let pressure: [String]
    /// Unix seconds.
    public let created: Int64

    public var id: String { name }

    public init(name: String, roles: [String] = [], ready: Bool = false, cordoned: Bool = false,
                internalIP: String? = nil, externalIP: String? = nil, kubelet: String? = nil, osImage: String? = nil,
                kernel: String? = nil, runtime: String? = nil, arch: String? = nil, pool: String? = nil,
                poolKind: String? = nil, instanceType: String? = nil, capacity: String? = nil, cpu: Double = 0,
                memory: Double = 0, podLimit: Int = 0, pressure: [String] = [], created: Int64 = 0) {
        self.name = name
        self.roles = roles
        self.ready = ready
        self.cordoned = cordoned
        self.internalIP = internalIP
        self.externalIP = externalIP
        self.kubelet = kubelet
        self.osImage = osImage
        self.kernel = kernel
        self.runtime = runtime
        self.arch = arch
        self.pool = pool
        self.poolKind = poolKind
        self.instanceType = instanceType
        self.capacity = capacity
        self.cpu = cpu
        self.memory = memory
        self.podLimit = podLimit
        self.pressure = pressure
        self.created = created
    }

    private enum CodingKeys: String, CodingKey {
        case name, roles, ready, cordoned, internalIP, externalIP, kubelet, osImage, kernel, runtime, arch
        case pool, poolKind, instanceType, capacity, cpu, memory, podLimit, pressure, created
    }

    // Go encodes empty (nil) slices as null and leaves the unknown strings out.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        roles = try c.field(.roles, [])
        ready = try c.field(.ready, false)
        cordoned = try c.field(.cordoned, false)
        internalIP = try c.decodeIfPresent(String.self, forKey: .internalIP)
        externalIP = try c.decodeIfPresent(String.self, forKey: .externalIP)
        kubelet = try c.decodeIfPresent(String.self, forKey: .kubelet)
        osImage = try c.decodeIfPresent(String.self, forKey: .osImage)
        kernel = try c.decodeIfPresent(String.self, forKey: .kernel)
        runtime = try c.decodeIfPresent(String.self, forKey: .runtime)
        arch = try c.decodeIfPresent(String.self, forKey: .arch)
        pool = try c.decodeIfPresent(String.self, forKey: .pool)
        poolKind = try c.decodeIfPresent(String.self, forKey: .poolKind)
        instanceType = try c.decodeIfPresent(String.self, forKey: .instanceType)
        capacity = try c.decodeIfPresent(String.self, forKey: .capacity)
        cpu = try c.field(.cpu, 0)
        memory = try c.field(.memory, 0)
        podLimit = try c.field(.podLimit, 0)
        pressure = try c.field(.pressure, [])
        created = try c.field(.created, 0)
    }

    /// The address to show: the internal one, else the external one.
    public var address: String? { internalIP ?? externalIP }

    /// Not ready, cordoned or under pressure: shown first, tinted.
    public var needsAttention: Bool { !ready || cordoned || !pressure.isEmpty }
}

/// What the user chose for one context of an imported kubeconfig (Go MergeKubeconfig
/// choices): left out (`skip`), or replacing the stored context of the same cluster.
public struct KubeImportChoice: Encodable, Equatable, Sendable {
    public let index: Int
    public var name: String?
    public var replace: Bool?
    public var skip: Bool?

    public init(index: Int, name: String? = nil, replace: Bool? = nil, skip: Bool? = nil) {
        self.index = index
        self.name = name
        self.replace = replace
        self.skip = skip
    }
}

/// The choices for an imported kubeconfig of `count` contexts (ParseKubeconfig's order): those
/// not `selected` are skipped, the selected ones in `replacing` replace the stored context of
/// the same cluster, the others take the name the core picks. Only contexts with a choice are listed.
public func kubeImportChoices(count: Int, selected: Set<Int>, replacing: Set<Int>) -> [KubeImportChoice] {
    (0..<max(count, 0)).compactMap { index in
        if !selected.contains(index) { return KubeImportChoice(index: index, skip: true) }
        if replacing.contains(index) { return KubeImportChoice(index: index, replace: true) }
        return nil
    }
}

/// The two stored configs (the talosconfig, and the kubeconfig of the clusters added without Talos).
public enum StoredConfigKind: String, CaseIterable, Sendable {
    case talosconfig, kubeconfig
}

/// A stored config as the app found it: none stored, read and parsed, or stored but not
/// readable (not decrypted, or not parsed). Same rule as Android's ConfigUnreadableException.
public enum StoredConfigStatus: Equatable, Sendable {
    case absent, loaded, unreadable

    public init(stored: Bool, parsed: Bool) {
        self = !stored ? .absent : parsed ? .loaded : .unreadable
    }
}

/// The stored configs that could not be read, in StoredConfigKind order. While one is listed the
/// app shows none of them and writes nothing: showing only the other store would let an import,
/// a removal or a restore overwrite or delete the one it could not read.
public func unreadableConfigs(_ statuses: [StoredConfigKind: StoredConfigStatus]) -> [StoredConfigKind] {
    StoredConfigKind.allCases.filter { statuses[$0] == .unreadable }
}

public extension ConfigSummary {
    /// The positions of the contexts that can be added (no problem): selected at first in an import preview.
    var importableIndices: Set<Int> {
        Set(contexts.indices.filter { contexts[$0].problem == nil })
    }
}
