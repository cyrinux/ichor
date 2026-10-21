import Foundation

// Mirrors go/ichorgo/kube_autoscaling.go: what scales the cluster (HPAs, KEDA, VPA,
// Karpenter) and the per-workload index, and the pure logic the Autoscaling screen filters
// it with. Quantities stay strings ("250m", "63%"): the Go side already decided the bools.

/// What drives a workload's replica count.
public enum AutoscaledBy: String, Sendable, WireEnum {
    case hpa = "hpa"
    case keda = "keda"
    case vpa = "vpa"
    case unknown = ""

    public static let wireFallback: Self = .unknown
}

/// The state an autoscaler row is in, problems first (the Go order).
public enum AutoscaleState: String, Sendable, WireEnum {
    case notReady = "notReady"
    case atMax = "atMax"
    case limited = "limited"
    case fallback = "fallback"
    case paused = "paused"
    case ok = "ok"
    case unknown = ""

    public static let wireFallback: Self = .unknown
}

/// Everything that scales the cluster (os:admin); a group the cluster lacks says so.
public struct AutoscalingReport: Decodable, Equatable, Hashable, Sendable {
    public let hpa: HpaGroup
    public let keda: KedaGroup
    public let vpa: VpaGroup
    public let karpenter: KarpenterGroup
    /// One entry per (workload, autoscaler kind), for the workload sheet's chip.
    public let workloads: [AutoscaledWorkload]
    public let workloadsTruncated: Bool

    public init(hpa: HpaGroup = HpaGroup(), keda: KedaGroup = KedaGroup(), vpa: VpaGroup = VpaGroup(),
                karpenter: KarpenterGroup = KarpenterGroup(), workloads: [AutoscaledWorkload] = [], workloadsTruncated: Bool = false) {
        self.hpa = hpa
        self.keda = keda
        self.vpa = vpa
        self.karpenter = karpenter
        self.workloads = workloads
        self.workloadsTruncated = workloadsTruncated
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        hpa = try c.field(.hpa, HpaGroup())
        keda = try c.field(.keda, KedaGroup())
        vpa = try c.field(.vpa, VpaGroup())
        karpenter = try c.field(.karpenter, KarpenterGroup())
        workloads = try c.field(.workloads, [])
        workloadsTruncated = try c.field(.workloadsTruncated, false)
    }

    private enum CodingKeys: String, CodingKey { case hpa, keda, vpa, karpenter, workloads, workloadsTruncated }

    /// The rows a `namespace` (nil: every one) and a `query` keep, group by group. Karpenter
    /// is cluster-level: its pools and claims show under every namespace.
    public func filtered(namespace: String?, query: String) -> AutoscalingReport {
        let q = query.trimmingCharacters(in: .whitespaces)
        func keep(_ texts: [String]) -> Bool {
            q.isEmpty || texts.contains { $0.localizedCaseInsensitiveContains(q) }
        }

        return AutoscalingReport(
            hpa: HpaGroup(installed: hpa.installed, version: hpa.version, error: hpa.error, truncated: hpa.truncated, items: hpa.items.filter {
                (namespace == nil || $0.namespace == namespace) && keep([$0.name, $0.namespace, $0.target.kind, $0.target.name])
            }),
            keda: KedaGroup(installed: keda.installed, version: keda.version, error: keda.error, truncated: keda.truncated,
                            scaledObjects: keda.scaledObjects.filter {
                                (namespace == nil || $0.namespace == namespace) && keep([$0.name, $0.namespace, $0.target.kind, $0.target.name] + $0.triggers.map(\.label))
                            },
                            scaledJobs: keda.scaledJobs.filter {
                                (namespace == nil || $0.namespace == namespace) && keep([$0.name, $0.namespace] + $0.triggers.map(\.label))
                            }),
            vpa: VpaGroup(installed: vpa.installed, version: vpa.version, error: vpa.error, truncated: vpa.truncated, items: vpa.items.filter {
                (namespace == nil || $0.namespace == namespace) && keep([$0.name, $0.namespace, $0.target.kind, $0.target.name, $0.updateMode])
            }),
            karpenter: KarpenterGroup(installed: karpenter.installed, version: karpenter.version, error: karpenter.error, truncated: karpenter.truncated,
                                      nodePools: karpenter.nodePools.filter { keep([$0.name, $0.capacityTypes.joined(separator: " ")]) },
                                      nodeClaims: karpenter.nodeClaims.filter { keep([$0.name, $0.pool, $0.nodeName, $0.instanceType, $0.capacityType]) }),
            workloads: workloads,
            workloadsTruncated: workloadsTruncated,
        )
    }

    /// The namespaces with autoscaling rows, for the namespace picker.
    public var namespaceNames: [String] {
        Array(Set(hpa.items.map(\.namespace) + keda.scaledObjects.map(\.namespace) + keda.scaledJobs.map(\.namespace) + vpa.items.map(\.namespace))
            .filter { !$0.isEmpty }).sorted()
    }
}

/// What every group says about itself before its rows.
public struct AutoscalingGroupState: Decodable, Equatable, Hashable, Sendable {
    public let installed: Bool
    public let version: String
    /// This group's read failed (often a 403): the other groups still show.
    public let error: String
    public let truncated: Bool

    public init(installed: Bool = false, version: String = "", error: String = "", truncated: Bool = false) {
        self.installed = installed
        self.version = version
        self.error = error
        self.truncated = truncated
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        truncated = try c.field(.truncated, false)
    }

    private enum CodingKeys: String, CodingKey { case installed, version, error, truncated }
}

public struct HpaGroup: Decodable, Equatable, Hashable, Sendable {
    public let installed: Bool
    public let version: String
    public let error: String
    public let truncated: Bool
    public let items: [HpaRow]

    public var state: AutoscalingGroupState { AutoscalingGroupState(installed: installed, version: version, error: error, truncated: truncated) }

    public init(installed: Bool = false, version: String = "", error: String = "", truncated: Bool = false, items: [HpaRow] = []) {
        self.installed = installed
        self.version = version
        self.error = error
        self.truncated = truncated
        self.items = items
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        truncated = try c.field(.truncated, false)
        items = try c.field(.items, [])
    }

    private enum CodingKeys: String, CodingKey { case installed, version, error, truncated, items }
}

public struct KedaGroup: Decodable, Equatable, Hashable, Sendable {
    public let installed: Bool
    public let version: String
    public let error: String
    public let truncated: Bool
    public let scaledObjects: [ScaledObjectRow]
    public let scaledJobs: [ScaledJobRow]

    public var state: AutoscalingGroupState { AutoscalingGroupState(installed: installed, version: version, error: error, truncated: truncated) }

    public init(installed: Bool = false, version: String = "", error: String = "", truncated: Bool = false,
                scaledObjects: [ScaledObjectRow] = [], scaledJobs: [ScaledJobRow] = []) {
        self.installed = installed
        self.version = version
        self.error = error
        self.truncated = truncated
        self.scaledObjects = scaledObjects
        self.scaledJobs = scaledJobs
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        truncated = try c.field(.truncated, false)
        scaledObjects = try c.field(.scaledObjects, [])
        scaledJobs = try c.field(.scaledJobs, [])
    }

    private enum CodingKeys: String, CodingKey { case installed, version, error, truncated, scaledObjects, scaledJobs }
}

public struct VpaGroup: Decodable, Equatable, Hashable, Sendable {
    public let installed: Bool
    public let version: String
    public let error: String
    public let truncated: Bool
    public let items: [VpaRow]

    public var state: AutoscalingGroupState { AutoscalingGroupState(installed: installed, version: version, error: error, truncated: truncated) }

    public init(installed: Bool = false, version: String = "", error: String = "", truncated: Bool = false, items: [VpaRow] = []) {
        self.installed = installed
        self.version = version
        self.error = error
        self.truncated = truncated
        self.items = items
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        truncated = try c.field(.truncated, false)
        items = try c.field(.items, [])
    }

    private enum CodingKeys: String, CodingKey { case installed, version, error, truncated, items }
}

public struct KarpenterGroup: Decodable, Equatable, Hashable, Sendable {
    public let installed: Bool
    public let version: String
    public let error: String
    public let truncated: Bool
    public let nodePools: [NodePoolRow]
    public let nodeClaims: [NodeClaimRow]

    public var state: AutoscalingGroupState { AutoscalingGroupState(installed: installed, version: version, error: error, truncated: truncated) }

    public init(installed: Bool = false, version: String = "", error: String = "", truncated: Bool = false,
                nodePools: [NodePoolRow] = [], nodeClaims: [NodeClaimRow] = []) {
        self.installed = installed
        self.version = version
        self.error = error
        self.truncated = truncated
        self.nodePools = nodePools
        self.nodeClaims = nodeClaims
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        truncated = try c.field(.truncated, false)
        nodePools = try c.field(.nodePools, [])
        nodeClaims = try c.field(.nodeClaims, [])
    }

    private enum CodingKeys: String, CodingKey { case installed, version, error, truncated, nodePools, nodeClaims }
}

/// The workload an autoscaler drives.
public struct AutoscaleTarget: Decodable, Equatable, Hashable, Sendable {
    public let kind: String
    public let name: String

    public var label: String { [kind, name].filter { !$0.isEmpty }.joined(separator: " ") }

    public init(kind: String = "", name: String = "") {
        self.kind = kind
        self.name = name
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        name = try c.field(.name, "")
    }

    private enum CodingKeys: String, CodingKey { case kind, name }
}

/// One (workload, autoscaler kind) pair: what the workload sheet names.
public struct AutoscaledWorkload: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let by: AutoscaledBy
    public let autoscaler: String
    public let min: Int
    public let max: Int
    public let current: Int
    public let desired: Int
    public let paused: Bool
    public let state: AutoscaleState

    public var id: String { "\(kind)/\(namespace)/\(name)/\(by.rawValue)" }

    public init(kind: String = "", namespace: String = "", name: String = "", by: AutoscaledBy = .unknown, autoscaler: String = "",
                min: Int = 0, max: Int = 0, current: Int = 0, desired: Int = 0, paused: Bool = false, state: AutoscaleState = .ok) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.by = by
        self.autoscaler = autoscaler
        self.min = min
        self.max = max
        self.current = current
        self.desired = desired
        self.paused = paused
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        by = try c.wire(.by)
        autoscaler = try c.field(.autoscaler, "")
        min = try c.field(.min, 0)
        max = try c.field(.max, 0)
        current = try c.field(.current, 0)
        desired = try c.field(.desired, 0)
        paused = try c.field(.paused, false)
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, by, autoscaler, min, max, current, desired, paused, state }
}

/// One problem row of the "Needs attention" section, across the four groups.
public struct AutoscalingProblem: Equatable, Hashable, Identifiable, Sendable {
    public enum Group: String, Sendable, WireEnum {
        case hpa = "hpa"
        case keda = "keda"
        case vpa = "vpa"
        case karpenter = "karpenter"
        case unknown = ""

        public static let wireFallback: Self = .unknown
    }

    public let group: Group
    /// The row's key inside its group ("ns/name", or the pool/claim name).
    public let key: String
    public let namespace: String
    public let name: String
    public let state: AutoscaleState
    /// The row's own message, "" when it has none.
    public let message: String

    public var id: String { "\(group.rawValue)/\(key)" }
    public var label: String { [namespace, name].filter { !$0.isEmpty }.joined(separator: "/") }

    init(group: Group, key: String, namespace: String, name: String, state: AutoscaleState, message: String) {
        self.group = group
        self.key = key
        self.namespace = namespace
        self.name = name
        self.state = state
        self.message = message
    }
}

extension AutoscalingReport {
    /// The rows that need attention, group by group in the Go order (problems first inside).
    public var problems: [AutoscalingProblem] {
        var out: [AutoscalingProblem] = []
        for row in hpa.items where row.state != .ok {
            out.append(AutoscalingProblem(group: .hpa, key: row.id, namespace: row.namespace, name: row.name, state: row.state, message: row.conditions.message))
        }
        for row in keda.scaledObjects where row.state != .ok {
            out.append(AutoscalingProblem(group: .keda, key: row.id, namespace: row.namespace, name: row.name, state: row.state, message: row.message))
        }
        for row in keda.scaledJobs where row.state != .ok {
            out.append(AutoscalingProblem(group: .keda, key: row.id, namespace: row.namespace, name: row.name, state: row.state, message: row.message))
        }
        for row in vpa.items where row.state != .ok {
            out.append(AutoscalingProblem(group: .vpa, key: row.id, namespace: row.namespace, name: row.name, state: row.state, message: row.message))
        }
        for row in karpenter.nodePools where row.atLimit || !row.ready {
            out.append(AutoscalingProblem(group: .karpenter, key: row.id, namespace: "", name: row.name, state: row.state, message: row.message))
        }
        for row in karpenter.nodeClaims where row.pending || row.drifted || row.disrupting || !row.ready {
            out.append(AutoscalingProblem(group: .karpenter, key: row.id, namespace: "", name: row.name, state: row.state, message: row.message))
        }
        return out
    }
}
