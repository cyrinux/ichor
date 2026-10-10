import Foundation

// Mirrors the JSON produced by the Go core (go/ichorgo), like the Android models.

public struct ConfigSummary: Decodable, Equatable, Sendable {
    public let current: String
    public let contexts: [ContextSummary]

    public init(current: String, contexts: [ContextSummary]) {
        self.current = current
        self.contexts = contexts
    }

    public func context(named name: String) -> ContextSummary? {
        contexts.first { $0.name == name }
    }

    /// The context to open: the saved position first (names differ while the screenshot mode
    /// masks them; Go keeps the order), then the saved name (installs from before the
    /// position was saved), then the config's current context. Same rule as Android.
    public func selectedContext(index: Int?, name: String?) -> String {
        if let index, contexts.indices.contains(index) { return contexts[index].name }
        return name.flatMap { context(named: $0)?.name } ?? current
    }

    /// The context `step` positions after `active` (before it when negative), nil past
    /// either end. Same rule as Android.
    public func adjacentContext(to active: String, step: Int) -> String? {
        guard step != 0, let index = contexts.firstIndex(where: { $0.name == active }),
              contexts.indices.contains(index + step) else { return nil }
        return contexts[index + step].name
    }

    /// One list of the stored clusters: the talosconfig's contexts, then the kubeconfig's
    /// (names are unique across both, the Go core sees to it). The current context is the
    /// talosconfig's, or the kubeconfig's when there is no talosconfig. Nil when neither is stored.
    public static func combined(talos: ConfigSummary?, kube: ConfigSummary?) -> ConfigSummary? {
        switch (talos, kube) {
        case (nil, nil): return nil
        case (let talos?, nil): return talos
        case (nil, let kube?): return kube
        case (let talos?, let kube?):
            let current = talos.contexts.isEmpty ? kube.current : talos.current
            return ConfigSummary(current: current, contexts: talos.contexts + kube.contexts)
        }
    }

    /// Where the active context is once the one at `removed` is gone, among the `remaining`
    /// ones: it keeps showing the same context, or the removed one's neighbour.
    public static func activeIndexAfterRemoval(active: Int, removed: Int, remaining: Int) -> Int {
        if active > removed { return active - 1 }
        if active == removed { return min(removed, remaining - 1) }
        return active
    }
}

public struct ContextSummary: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// "talos" for a talosconfig context, "kube" for one added from a kubeconfig (no Talos API).
    public let kind: String
    /// Identifies the cluster whatever the screenshot mode does to `name`; keys its color.
    public let fingerprint: String
    /// The same for every context of the cluster, on any phone: what share links name.
    public let clusterID: String
    /// Talos endpoints; the API server URL for a kubeconfig context.
    public let endpoints: [String]
    public let nodes: [String]
    public let roles: [String]
    /// Client certificate expiry, or a kubeconfig token's `exp` (0: unknown).
    public let certNotAfter: Int64
    public let demo: Bool
    /// Kubeconfig contexts only (ParseKubeconfig): the default namespace, how it signs in
    /// (cert, token, eks, oidc…) and to what, who the credentials are, and why it cannot be
    /// added (a kube-… problem code, nil when it can).
    public let namespace: String?
    public let auth: String?
    public let authDetail: String?
    public let user: String?
    public let insecure: Bool
    public let problem: String?
    public let problemDetail: String?
    /// The method the app signs this kubeconfig context in with (oidc, eks, gke, azure,
    /// digitalocean, rancher), nil for static credentials (see KubeSignInInfo).
    public let signIn: String?
    /// A Talos context signed in through Sidero Omni (no certificate): who it signs as, and
    /// the Omni cluster. `signIn` is then omni or omni-service-account.
    public let omni: Bool
    public let identity: String?
    public let cluster: String?
    /// Where an Omni context's sign-in is kept (shared by an identity's clusters on one
    /// instance): kept and backed up with the cluster, see authStoreFingerprints.
    public let authKey: String?

    public var id: String { name }

    /// Added from a kubeconfig: the Kubernetes API only, no Talos.
    public var isKube: Bool { kind == ContextKind.kube }

    /// A Talos cluster that lists no endpoint (a talosconfig generated before it had
    /// addresses): nothing to reach until one is added or found on the network.
    public var needsEndpoint: Bool { endpoints.isEmpty && !demo && !isKube }

    public init(name: String, kind: String = ContextKind.talos, fingerprint: String = "", clusterID: String = "",
                endpoints: [String] = [], nodes: [String] = [], roles: [String] = [], certNotAfter: Int64 = 0,
                demo: Bool = false, namespace: String? = nil, auth: String? = nil, authDetail: String? = nil,
                user: String? = nil, insecure: Bool = false, problem: String? = nil, problemDetail: String? = nil,
                signIn: String? = nil, omni: Bool = false, identity: String? = nil, cluster: String? = nil, authKey: String? = nil) {
        self.name = name
        self.kind = kind
        self.fingerprint = fingerprint
        self.clusterID = clusterID
        self.endpoints = endpoints
        self.nodes = nodes
        self.roles = roles
        self.certNotAfter = certNotAfter
        self.demo = demo
        self.namespace = namespace
        self.auth = auth
        self.authDetail = authDetail
        self.user = user
        self.insecure = insecure
        self.problem = problem
        self.problemDetail = problemDetail
        self.signIn = signIn
        self.omni = omni
        self.identity = identity
        self.cluster = cluster
        self.authKey = authKey
    }

    private enum CodingKeys: String, CodingKey {
        case name, kind, fingerprint, endpoints, nodes, roles, certNotAfter, demo
        case namespace, auth, authDetail, user, insecure, problem, problemDetail, signIn
        case omni, identity, cluster, authKey
        case clusterID = "clusterId"
    }

    // Go encodes empty (nil) slices as null here; older cores send no kind (Talos then).
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        kind = try c.field(.kind, ContextKind.talos)
        fingerprint = try c.field(.fingerprint, "")
        clusterID = try c.field(.clusterID, "")
        endpoints = try c.field(.endpoints, [])
        nodes = try c.field(.nodes, [])
        roles = try c.field(.roles, [])
        certNotAfter = try c.field(.certNotAfter, 0)
        demo = try c.field(.demo, false)
        namespace = try c.decodeIfPresent(String.self, forKey: .namespace)
        auth = try c.decodeIfPresent(String.self, forKey: .auth)
        authDetail = try c.decodeIfPresent(String.self, forKey: .authDetail)
        user = try c.decodeIfPresent(String.self, forKey: .user)
        insecure = try c.field(.insecure, false)
        problem = try c.decodeIfPresent(String.self, forKey: .problem)
        problemDetail = try c.decodeIfPresent(String.self, forKey: .problemDetail)
        signIn = try c.decodeIfPresent(String.self, forKey: .signIn).flatMap { $0.isEmpty ? nil : $0 }
        omni = try c.field(.omni, false)
        identity = try c.decodeIfPresent(String.self, forKey: .identity)
        cluster = try c.decodeIfPresent(String.self, forKey: .cluster)
        authKey = try c.decodeIfPresent(String.self, forKey: .authKey).flatMap { $0.isEmpty ? nil : $0 }
    }
}

/// The kinds of cluster (ContextSummary.kind).
public enum ContextKind {
    public static let talos = "talos"
    public static let kube = "kube"
}

public struct ClusterOverview: Codable, Equatable, Sendable {
    public let context: String
    public let nodes: [NodeOverview]

    public init(context: String, nodes: [NodeOverview]) {
        self.context = context
        self.nodes = nodes
    }
}

public enum NodeHealth: String, Sendable, CaseIterable {
    case ready, notReady, unreachable
}

/// Codable: the last known overview is stored as it was shown (see mergeLastKnown).
public struct NodeOverview: Codable, Equatable, Identifiable, Hashable, Sendable {
    public let node: String
    public let hostname: String
    public let reachable: Bool
    public let error: String?
    /// Why an unreachable node failed: network, tls, auth or other; nil from older cores.
    public let errorKind: String?
    public let version: String
    public let arch: String
    public let platform: String
    public let role: String
    public let stage: String
    public let ready: Bool
    public let unmetConditions: [UnmetCondition]
    /// Capacity; 0 when the node did not say.
    public let cpuCount: Int
    public let memTotal: UInt64 // bytes
    public let memAvailable: UInt64 // bytes
    /// Internet-facing addresses, IPv4 first; empty when none (or from an older core).
    public let publicIPs: [String]
    /// An unreachable node shown with what it said last: when that was (epoch ms). Never
    /// from Go, set by mergeLastKnown.
    public let lastSeen: Int64?
    /// The Kubernetes node is unschedulable, as the node itself says; see `cordonKnown`.
    public let cordoned: Bool
    /// False when the node did not say (older Talos, not a Kubernetes member yet).
    public let cordonKnown: Bool

    public var id: String { node }

    public var health: NodeHealth {
        if !reachable { return .unreachable }
        return ready ? .ready : .notReady
    }

    /// Worth a full row even in the collapsed nodes section: down, not ready, or reporting a problem.
    public var needsAttention: Bool {
        health != .ready || !unmetConditions.isEmpty || !(error ?? "").trimmingCharacters(in: .whitespaces).isEmpty || cordoned
    }

    public init(
        node: String, hostname: String, reachable: Bool, error: String? = nil, errorKind: String? = nil,
        version: String = "", arch: String = "", platform: String = "", role: String = "", stage: String = "",
        ready: Bool = false, unmetConditions: [UnmetCondition] = [], cpuCount: Int = 0, memTotal: UInt64 = 0,
        memAvailable: UInt64 = 0, publicIPs: [String] = [], lastSeen: Int64? = nil, cordoned: Bool = false,
        cordonKnown: Bool = false
    ) {
        self.node = node
        self.hostname = hostname
        self.reachable = reachable
        self.error = error
        self.errorKind = errorKind
        self.version = version
        self.arch = arch
        self.platform = platform
        self.role = role
        self.stage = stage
        self.ready = ready
        self.unmetConditions = unmetConditions
        self.cpuCount = cpuCount
        self.memTotal = memTotal
        self.memAvailable = memAvailable
        self.publicIPs = publicIPs
        self.lastSeen = lastSeen
        self.cordoned = cordoned
        self.cordonKnown = cordonKnown
    }

    private enum CodingKeys: String, CodingKey {
        case node, hostname, reachable, error, errorKind, version, arch, platform, role, stage, ready
        case unmetConditions, cpuCount, memTotal, memAvailable, publicIPs, lastSeen, cordoned, cordonKnown
    }

    // Older cores send no capacity; Go encodes an empty (nil) slice as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decode(String.self, forKey: .node)
        hostname = try c.decode(String.self, forKey: .hostname)
        reachable = try c.decode(Bool.self, forKey: .reachable)
        error = try c.decodeIfPresent(String.self, forKey: .error)
        errorKind = try c.decodeIfPresent(String.self, forKey: .errorKind)
        version = try c.decode(String.self, forKey: .version)
        arch = try c.decode(String.self, forKey: .arch)
        platform = try c.decode(String.self, forKey: .platform)
        role = try c.decode(String.self, forKey: .role)
        stage = try c.decode(String.self, forKey: .stage)
        ready = try c.decode(Bool.self, forKey: .ready)
        unmetConditions = try c.field(.unmetConditions, [])
        cpuCount = try c.field(.cpuCount, 0)
        memTotal = try c.field(.memTotal, 0)
        memAvailable = try c.field(.memAvailable, 0)
        publicIPs = try c.field(.publicIPs, [])
        lastSeen = try c.decodeIfPresent(Int64.self, forKey: .lastSeen)
        cordoned = try c.field(.cordoned, false)
        cordonKnown = try c.field(.cordonKnown, false)
    }
}

public struct UnmetCondition: Codable, Equatable, Hashable, Sendable {
    public let name: String
    public let reason: String

    public init(name: String, reason: String) {
        self.name = name
        self.reason = reason
    }
}

public struct ServiceInfo: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let state: String
    public let health: String // healthy | unhealthy | unknown
    public let message: String?
    public let lastEvent: String?
    public let lastChange: Int64?
}

public struct NodeResources: Decodable, Equatable, Sendable {
    public let memTotal: UInt64
    public let memAvailable: UInt64
    public let load1: Double
    public let load5: Double
    public let load15: Double
    public let bootTime: UInt64
    public let cpuCount: Int
    public let cpuModel: String
    public let mounts: [MountUsage]
}

public struct MountUsage: Decodable, Equatable, Identifiable, Sendable {
    public let filesystem: String
    public let mountedOn: String
    public let size: UInt64
    public let available: UInt64

    public var id: String { mountedOn }
}

public struct EtcdOverview: Decodable, Equatable, Sendable {
    public let error: String?
    public let leaderId: String
    public let members: [EtcdMember]
    public let statuses: [EtcdNodeStatus]
    public let alarms: [EtcdAlarm]
    /// Set when no control-plane node answered the alarm list: `alarms` is unknown, not empty.
    public let alarmsError: String?
}

public struct EtcdMember: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let hostname: String
    public let peerUrls: [String]
    public let clientUrls: [String]
    public let isLearner: Bool
}

public struct EtcdNodeStatus: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let error: String?
    public let memberId: String
    public let isLeader: Bool
    public let isLearner: Bool
    public let dbSize: Int64
    public let dbSizeInUse: Int64
    public let raftIndex: UInt64
    public let raftTerm: UInt64
    /// Trails `raftIndex` (committed) while the member applies its backlog; nil from older cores.
    public let raftAppliedIndex: UInt64?
    public let version: String
    public let errors: [String]

    public var id: String { node }
}

public struct EtcdAlarm: Decodable, Equatable, Hashable, Sendable {
    public let memberId: String
    public let alarm: String
}

public struct LogTail: Decodable, Equatable, Sendable {
    public let lines: [String]
    public let truncated: Bool
    /// Parsed `lines` (same order and count); nil from an older core or when they do not
    /// decode, see logEntries.
    public let entries: [LogEntry]?

    private enum CodingKeys: String, CodingKey { case lines, truncated, entries }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        lines = try c.field(.lines, [])
        truncated = try c.field(.truncated, false)
        entries = try? c.decodeIfPresent([LogEntry].self, forKey: .entries)
    }
}

/// Features gated by Talos RBAC (rules from Talos v1.14 machined.go).
public enum Feature: CaseIterable, Sendable {
    case power, health, kubeconfig, debugShell, etcdDefrag, machineConfig, etcdSnapshot, serviceControl, issueConfig, packetCapture, upgrade
    case etcdMemberActions, resourceBrowser, supportBundle, workloads, cgroups, reset, containerRestart

    public var label: String {
        switch self {
        case .power: "Reboot / shutdown"
        case .health: "Cluster health check"
        case .kubeconfig: "Kubeconfig export"
        case .debugShell: "Debug shell"
        case .etcdDefrag: "etcd defragmentation"
        case .machineConfig: "Machine config"
        case .etcdSnapshot: "etcd snapshot"
        case .serviceControl: "Service control"
        case .issueConfig: "Issue talosconfig"
        case .packetCapture: "Packet capture"
        case .upgrade: "Talos upgrade"
        case .etcdMemberActions: "etcd member actions"
        case .resourceBrowser: "Resources browser"
        case .supportBundle: "Support bundle"
        case .workloads: "Kubernetes workloads"
        case .cgroups: "Cgroups and pressure"
        case .reset: "Node reset"
        case .containerRestart: "Container restart"
        }
    }

    public var roles: Set<String> {
        switch self {
        case .power, .etcdDefrag, .serviceControl, .packetCapture: ["os:admin", "os:operator"]
        // The server-side health check fetches a Kubernetes admin kubeconfig with the caller's role.
        // DebugService/ContainerRun is admin-only too.
        // The machine config holds the cluster secrets (CA keys, tokens).
        // Issuing a client certificate (GenerateClientConfiguration) is admin-only.
        // MachineService/Upgrade is admin-only.
        // Forfeiting leadership and removing a member (EtcdForfeitLeadership, EtcdRemoveMemberByID)
        // are admin-only.
        // The Kubernetes API is reached with the admin kubeconfig Talos only issues to os:admin.
        // The cgroup tree is a MachineService/Copy of /sys/fs/cgroup, admin-only too.
        // Restarting one container (MachineService/Restart) is admin-only.
        case .health, .kubeconfig, .debugShell, .machineConfig, .issueConfig, .upgrade, .etcdMemberActions, .workloads, .cgroups, .reset,
             .containerRestart: ["os:admin"]
        // Reading resources (COSI state) is open to every role; Talos itself filters what a
        // reader may see of the sensitive ones. A support bundle too: the parts the role cannot
        // read (machine config: os:admin, etcd status: os:operator) are left out and noted in it.
        case .resourceBrowser, .supportBundle: ["os:admin", "os:operator", "os:reader"]
        // A snapshot holds every Kubernetes Secret; Talos has a dedicated role for it.
        case .etcdSnapshot: ["os:admin", "os:operator", "os:etcd:backup"]
        }
    }

    /// What a cluster reached through Omni cannot do: Omni issues its talosconfigs, not Talos.
    public static let omniUnavailable: Set<Feature> = [.issueConfig]

    /// What a cluster added from a kubeconfig can use: the Kubernetes API, and its kubeconfig.
    public static let kubernetes: Set<Feature> = [.workloads, .kubeconfig]

    public var minimumRole: String {
        if roles.contains("os:reader") { return "os:reader" }
        return roles.contains("os:operator") ? "os:operator" : "os:admin"
    }
}

public extension ContextSummary {
    /// A Talos context by its roles; one added from a kubeconfig has the Kubernetes features
    /// only (its own RBAC answers for them), never a Talos one.
    func allows(_ feature: Feature) -> Bool {
        if isKube { return Feature.kubernetes.contains(feature) }
        // Omni applies the user's own role to every call; Kubernetes goes through its kube proxy.
        if omni { return !Feature.omniUnavailable.contains(feature) }
        return roles.contains { feature.roles.contains($0) }
    }
}

public enum TalosJSON {
    public static func decode<T: Decodable>(_ type: T.Type, from json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    /// `value` as the JSON string the Go core takes.
    public static func encode<T: Encodable>(_ value: T) throws -> String {
        String(decoding: try JSONEncoder().encode(value), as: UTF8.self)
    }
}

public struct KubeSpanOverview: Decodable, Equatable, Sendable {
    public let nodes: [KubeSpanNode]
}

public struct KubeSpanNode: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let error: String?
    public let enabled: Bool
    public let up: Int
    public let down: Int
    public let peers: [KubeSpanPeer]

    public var id: String { node }
}

public struct KubeSpanPeer: Decodable, Equatable, Identifiable, Sendable {
    public let publicKey: String
    public let label: String
    public let state: String // up | down | unknown
    public let endpoint: String
    public let rx: Int64
    public let tx: Int64
    public let lastHandshake: Int64

    public var id: String { publicKey }
}

public extension EtcdNodeStatus {
    /// Space a defragmentation would give back (on-disk size minus space in use).
    var reclaimable: Int64 { max(dbSize - dbSizeInUse, 0) }
}

/// Members to defragment one at a time, as Talos advises: followers first, the leader last,
/// skipping members that could not be queried (same rule as Android).
public func defragOrder(_ statuses: [EtcdNodeStatus]) -> [EtcdNodeStatus] {
    statuses
        .filter { $0.error == nil && !$0.memberId.isEmpty }
        .sorted { a, b in
            if a.isLeader != b.isLeader { return !a.isLeader }
            return a.reclaimable > b.reclaimable
        }
}

public extension Array where Element == NodeOverview {
    /// The Talos version every answering node runs, which the cluster summary already shows, so
    /// the node rows can leave it out; nil while they differ or none answered.
    var sharedVersion: String? {
        let versions = Set(filter(\.reachable).map(\.version))
        guard versions.count == 1, let version = versions.first, !version.isEmpty else { return nil }
        return version
    }
}
