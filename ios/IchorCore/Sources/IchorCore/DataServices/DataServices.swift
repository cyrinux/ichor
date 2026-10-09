import Foundation

// Mirrors go/ichorgo/kube_dataservices.go, kube_longhorn.go, kube_garage.go, kube_cnpg.go and kube_certmanager.go
// (the wire format is documented in plans/data-services/README.md).
// One file per service in DataServices/; this one keeps the envelope and what they share.

/// Health of the storage and database operators a cluster runs; a nil section is not installed.
public struct DataServices: Decodable, Equatable, Sendable {
    public let longhorn: LonghornStatus?
    public let garage: GarageStatus?
    public let cnpg: CnpgStatus?
    public let dragonfly: DragonflyStatus?
    public let mariadb: MariaDbStatus?
    public let percona: PerconaStatus?
    public let certManager: CertManagerStatus?
    public let velero: VeleroStatus?
    public let ceph: CephStatus?
    public let castai: CastAIStatus?

    public init(longhorn: LonghornStatus? = nil, garage: GarageStatus? = nil, cnpg: CnpgStatus? = nil, dragonfly: DragonflyStatus? = nil,
                mariadb: MariaDbStatus? = nil, percona: PerconaStatus? = nil, certManager: CertManagerStatus? = nil,
                velero: VeleroStatus? = nil, ceph: CephStatus? = nil, castai: CastAIStatus? = nil) {
        self.longhorn = longhorn
        self.garage = garage
        self.cnpg = cnpg
        self.dragonfly = dragonfly
        self.mariadb = mariadb
        self.percona = percona
        self.certManager = certManager
        self.velero = velero
        self.ceph = ceph
        self.castai = castai
    }
}

/// One pod of a database or cache instance, as every data-service operator reports it.
public struct ServicePod: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// "" while Pending: not scheduled anywhere.
    public let node: String
    public let phase: String
    /// The operator's role label (master/replica, primary/replica, member for Galera), "" when
    /// unknown or the operator has none (Percona).
    public let role: String
    public let ready: Bool

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        node = try c.field(.node, "")
        phase = try c.field(.phase, "")
        role = try c.field(.role, "")
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey { case name, node, phase, role, ready }
}

/// Health of one item (a volume, a Postgres cluster), worst first.
public enum ServiceHealth: Int, Sendable, Comparable, CaseIterable, WireDecodable {
    case critical, warning, unknown, ok, idle

    public init(wire: String) {
        switch wire {
        case "critical": self = .critical
        case "warning": self = .warning
        case "ok": self = .ok
        case "idle": self = .idle
        default: self = .unknown
        }
    }

    public var needsAttention: Bool { self == .critical || self == .warning }

    public static func < (a: ServiceHealth, b: ServiceHealth) -> Bool { a.rawValue < b.rawValue }

    /// The worst of healths, ok when there is none (or only idle ones).
    public static func worst<S: Sequence>(_ healths: S) -> ServiceHealth where S.Element == ServiceHealth {
        guard let worst = healths.min(), worst != .idle else { return .ok }
        return worst
    }
}

/// The systems, in display order, with their app catalog id.
public enum DataServiceKind: String, Sendable, CaseIterable, Identifiable, Hashable {
    case longhorn = "longhorn"
    case garage = "garage"
    case cnpg = "cloudnative-pg"
    case dragonfly = "dragonfly"
    case mariadb = "mariadb"
    case percona = "percona-xtradb"
    case certManager = "cert-manager"
    case velero = "velero"
    case ceph = "rook"
    case castai = "castai"

    public var id: String { rawValue }
    public var catalogID: String { rawValue }

    /// Product names: never translated.
    public var title: String {
        switch self {
        case .longhorn: "Longhorn"
        case .garage: "Garage"
        case .cnpg: "CloudNativePG"
        case .dragonfly: "Dragonfly"
        case .mariadb: "MariaDB"
        case .percona: "Percona XtraDB Cluster"
        case .certManager: "cert-manager"
        case .velero: "Velero"
        case .ceph: "Rook Ceph"
        case .castai: "CAST AI"
        }
    }

    /// The segmented picker's name: short enough for four segments.
    public var tabTitle: String {
        switch self {
        case .cnpg: "CNPG"
        case .percona: "Percona"
        default: title
        }
    }
}

/// The catalog ids among the inventory's apps, for KubeDataServices: "" when none runs.
public func dataServiceHints(_ inventory: ClusterInventory) -> String {
    let ids = Set(inventory.apps.map(\.id))
    return DataServiceKind.allCases.map(\.catalogID).filter(ids.contains).joined(separator: ",")
}

/// One system at a glance: items, how many need attention, the worst health, the error if unreadable.
public struct ServiceSummary: Equatable, Sendable {
    public let total: Int
    public let attention: Int
    public let health: ServiceHealth
    public let error: String

    public init(total: Int, attention: Int, health: ServiceHealth, error: String = "") {
        self.total = total
        self.attention = attention
        self.health = health
        self.error = error
    }
}

/// A node that is not ready and the problems it most likely explains.
public struct LikelyCause: Equatable, Sendable, Identifiable {
    public let node: String
    public let problems: Int
    public var id: String { node }

    public init(node: String, problems: Int) {
        self.node = node
        self.problems = problems
    }
}

public extension DataServices {
    /// The installed systems, in display order.
    var detected: [DataServiceKind] {
        [longhorn != nil ? .longhorn : nil, garage != nil ? .garage : nil, cnpg != nil ? .cnpg : nil,
         dragonfly != nil ? .dragonfly : nil, mariadb != nil ? .mariadb : nil, percona != nil ? .percona : nil,
         certManager != nil ? .certManager : nil, velero != nil ? .velero : nil,
         ceph != nil ? .ceph : nil, castai != nil ? .castai : nil].compactMap { $0 }
    }

    func summary(_ kind: DataServiceKind) -> ServiceSummary? {
        switch kind {
        case .longhorn:
            guard let lh = longhorn else { return nil }
            if !lh.error.isEmpty { return ServiceSummary(total: lh.volumes.count, attention: 0, health: .unknown, error: lh.error) }
            // A node that is not ready or a backup target that is gone needs a look as much as a volume.
            let healths = lh.volumes.map(\.health) + lh.nodes.filter { !$0.ready }.map { _ in ServiceHealth.warning } +
                lh.backupTargets.filter { !$0.available }.map { _ in ServiceHealth.warning }
            return ServiceSummary(total: lh.volumes.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths))
        case .garage:
            guard let g = garage else { return nil }
            if !g.error.isEmpty { return ServiceSummary(total: g.instances.count, attention: 0, health: .unknown, error: g.error) }
            let healths = g.instances.map(\.state.health)
            return ServiceSummary(total: g.instances.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths))
        case .cnpg:
            guard let p = cnpg else { return nil }
            if !p.error.isEmpty && p.clusters.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: p.error) }
            let healths = p.clusters.map(\.health)
            return ServiceSummary(total: p.clusters.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: p.error)
        case .dragonfly:
            guard let d = dragonfly else { return nil }
            if !d.error.isEmpty && d.instances.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: d.error) }
            let healths = d.instances.map(\.health)
            return ServiceSummary(total: d.instances.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: d.error)
        case .mariadb:
            return mariadb?.summary
        case .percona:
            guard let p = percona else { return nil }
            if !p.error.isEmpty && p.clusters.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: p.error) }
            let healths = p.clusters.map(\.health)
            return ServiceSummary(total: p.clusters.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: p.error)
        case .certManager:
            guard let cm = certManager else { return nil }
            if !cm.error.isEmpty && cm.certificates.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: cm.error) }
            // An issuer that is not ready needs a look as much as a certificate.
            let healths = cm.certificates.map(\.health) + cm.issuers.map(\.health)
            return ServiceSummary(total: cm.certificates.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: cm.error)
        case .velero:
            // The total counts the schedules; a storage location down or a failed backup taken by hand needs a look too.
            guard let v = velero else { return nil }
            if !v.error.isEmpty && v.schedules.isEmpty && v.locations.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: v.error) }
            let healths = v.schedules.map(\.health) + v.locations.map(\.health) + v.adhoc.map(\.health)
            return ServiceSummary(total: v.schedules.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: v.error)
        case .ceph:
            guard let c = ceph else { return nil }
            if !c.error.isEmpty && c.clusters.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: c.error) }
            // Clusters are the items; a pool that is not ready needs a look as much as a cluster.
            let healths = c.clusters.map(\.health) + c.pools.map(\.health)
            return ServiceSummary(total: c.clusters.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: c.error)
        case .castai:
            return castai?.summary
        }
    }

    /// Worst health over every installed system.
    var worst: ServiceHealth { .worst(detected.compactMap { summary($0)?.health }) }

    /// Nodes that are not ready (downNodes, plus the ones Longhorn reports) and how many problems each
    /// explains: a volume with a replica there, a Garage cluster whose missing node runs there, a Postgres
    /// cluster with an instance there that is not ready. Worst first; empty when no problem points to a node.
    func likelyCauses(downNodes: Set<String> = []) -> [LikelyCause] {
        let down = downNodes.union((longhorn?.nodes ?? []).filter { !$0.ready }.map(\.name))
        guard !down.isEmpty else { return [] }
        var hits: [String: Int] = [:]
        func count(_ nodes: [String]) { for node in Set(nodes) where down.contains(node) { hits[node, default: 0] += 1 } }

        for volume in longhorn?.volumes ?? [] where volume.health.needsAttention { count(volume.replicaNodes) }
        for inst in garage?.instances ?? [] where inst.state.health.needsAttention {
            count(inst.nodes.filter { !$0.up && $0.storage }.flatMap { [$0.kubeNode] + $0.tags })
        }
        for cluster in cnpg?.clusters ?? [] where cluster.health.needsAttention {
            count(cluster.instancePods.filter { !$0.ready }.map(\.node))
        }
        for instance in dragonfly?.instances ?? [] where instance.health.needsAttention {
            count(instance.pods.filter { !$0.ready }.map(\.node))
        }
        for cluster in mariadb?.clusters ?? [] where cluster.health.needsAttention {
            count(cluster.pods.filter { !$0.ready }.map(\.node))
        }
        for cluster in percona?.clusters ?? [] where cluster.health.needsAttention {
            count(cluster.pods.filter { !$0.ready }.map(\.node))
        }
        for cluster in ceph?.clusters ?? [] where cluster.health.needsAttention { count(cluster.notReadyNodes) }
        return hits.map { LikelyCause(node: $0.key, problems: $0.value) }
            .sorted { $0.problems != $1.problems ? $0.problems > $1.problems : $0.node < $1.node }
    }
}
