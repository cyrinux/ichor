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
public struct AutoscalingReport: Decodable, Equatable, Sendable {
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
public struct AutoscalingGroupState: Decodable, Equatable, Sendable {
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

public struct HpaGroup: Decodable, Equatable, Sendable {
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

public struct KedaGroup: Decodable, Equatable, Sendable {
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

public struct VpaGroup: Decodable, Equatable, Sendable {
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

public struct KarpenterGroup: Decodable, Equatable, Sendable {
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
public struct AutoscaleTarget: Decodable, Equatable, Sendable {
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

/// One HorizontalPodAutoscaler. One KEDA created (managedBy keda) links its ScaledObject.
public struct HpaRow: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let target: AutoscaleTarget
    public let min: Int
    public let max: Int
    public let current: Int
    public let desired: Int
    public let metrics: [HpaMetric]
    public let conditions: HpaConditions
    public let atMax: Bool
    /// Unix ms, 0 when it never scaled.
    public let lastScaleAt: Int64
    public let managedBy: String
    public let scaledObject: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(namespace: String = "", name: String = "", target: AutoscaleTarget = AutoscaleTarget(), min: Int = 0, max: Int = 0,
                current: Int = 0, desired: Int = 0, metrics: [HpaMetric] = [], conditions: HpaConditions = HpaConditions(),
                atMax: Bool = false, lastScaleAt: Int64 = 0, managedBy: String = "", scaledObject: String = "", state: AutoscaleState = .ok) {
        self.namespace = namespace
        self.name = name
        self.target = target
        self.min = min
        self.max = max
        self.current = current
        self.desired = desired
        self.metrics = metrics
        self.conditions = conditions
        self.atMax = atMax
        self.lastScaleAt = lastScaleAt
        self.managedBy = managedBy
        self.scaledObject = scaledObject
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        target = try c.field(.target, AutoscaleTarget())
        min = try c.field(.min, 0)
        max = try c.field(.max, 0)
        current = try c.field(.current, 0)
        desired = try c.field(.desired, 0)
        metrics = try c.field(.metrics, [])
        conditions = try c.field(.conditions, HpaConditions())
        atMax = try c.field(.atMax, false)
        lastScaleAt = try c.field(.lastScaleAt, 0)
        managedBy = try c.field(.managedBy, "")
        scaledObject = try c.field(.scaledObject, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, target, min, max, current, desired, metrics, conditions, atMax, lastScaleAt, managedBy, scaledObject, state
    }
}

/// One metric the HPA scales on: current against target, as the user reads them.
public struct HpaMetric: Decodable, Equatable, Identifiable, Sendable {
    public let type: String
    public let name: String
    public let current: String
    public let target: String

    /// "cpu" for a Resource metric, else "External s0-prometheus".
    public var label: String { type == "Resource" ? name : [type, name].filter { !$0.isEmpty }.joined(separator: " ") }
    public var id: String { "\(type)/\(name)" }

    public init(type: String = "", name: String = "", current: String = "", target: String = "") {
        self.type = type
        self.name = name
        self.current = current
        self.target = target
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        name = try c.field(.name, "")
        current = try c.field(.current, "")
        target = try c.field(.target, "")
    }

    private enum CodingKeys: String, CodingKey { case type, name, current, target }
}

public struct HpaConditions: Decodable, Equatable, Sendable {
    public let ableToScale: Bool
    public let scalingActive: Bool
    public let scalingLimited: Bool
    public let message: String

    public init(ableToScale: Bool = false, scalingActive: Bool = false, scalingLimited: Bool = false, message: String = "") {
        self.ableToScale = ableToScale
        self.scalingActive = scalingActive
        self.scalingLimited = scalingLimited
        self.message = message
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ableToScale = try c.field(.ableToScale, false)
        scalingActive = try c.field(.scalingActive, false)
        scalingLimited = try c.field(.scalingLimited, false)
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case ableToScale, scalingActive, scalingLimited, message }
}

/// One KEDA ScaledObject; current and desired come from the HPA it owns.
public struct ScaledObjectRow: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let target: AutoscaleTarget
    public let min: Int
    public let max: Int
    public let current: Int
    public let desired: Int
    public let triggers: [KedaTrigger]
    public let fallbackReplicas: Int
    public let paused: Bool
    /// Set when paused at a count.
    public let pausedReplicas: Int?
    public let ready: Bool
    public let active: Bool
    public let fallback: Bool
    public let message: String
    public let hpaName: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(namespace: String = "", name: String = "", target: AutoscaleTarget = AutoscaleTarget(), min: Int = 0, max: Int = 0,
                current: Int = 0, desired: Int = 0, triggers: [KedaTrigger] = [], fallbackReplicas: Int = 0, paused: Bool = false,
                pausedReplicas: Int? = nil, ready: Bool = false, active: Bool = false, fallback: Bool = false, message: String = "",
                hpaName: String = "", state: AutoscaleState = .ok) {
        self.namespace = namespace
        self.name = name
        self.target = target
        self.min = min
        self.max = max
        self.current = current
        self.desired = desired
        self.triggers = triggers
        self.fallbackReplicas = fallbackReplicas
        self.paused = paused
        self.pausedReplicas = pausedReplicas
        self.ready = ready
        self.active = active
        self.fallback = fallback
        self.message = message
        self.hpaName = hpaName
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        target = try c.field(.target, AutoscaleTarget())
        min = try c.field(.min, 0)
        max = try c.field(.max, 0)
        current = try c.field(.current, 0)
        desired = try c.field(.desired, 0)
        triggers = try c.field(.triggers, [])
        fallbackReplicas = try c.field(.fallbackReplicas, 0)
        paused = try c.field(.paused, false)
        pausedReplicas = try c.field(.pausedReplicas, Int?.none)
        ready = try c.field(.ready, false)
        active = try c.field(.active, false)
        fallback = try c.field(.fallback, false)
        message = try c.field(.message, "")
        hpaName = try c.field(.hpaName, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, target, min, max, current, desired, triggers, fallbackReplicas, paused, pausedReplicas, ready, active, fallback, message, hpaName, state
    }
}

public struct ScaledJobRow: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let triggers: [KedaTrigger]
    public let maxReplicaCount: Int
    public let paused: Bool
    public let ready: Bool
    public let message: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(namespace: String = "", name: String = "", triggers: [KedaTrigger] = [], maxReplicaCount: Int = 0, paused: Bool = false,
                ready: Bool = false, message: String = "", state: AutoscaleState = .ok) {
        self.namespace = namespace
        self.name = name
        self.triggers = triggers
        self.maxReplicaCount = maxReplicaCount
        self.paused = paused
        self.ready = ready
        self.message = message
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        triggers = try c.field(.triggers, [])
        maxReplicaCount = try c.field(.maxReplicaCount, 0)
        paused = try c.field(.paused, false)
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, triggers, maxReplicaCount, paused, ready, message, state }
}

/// One trigger: its type and the one setting the user recognises. Never a secret.
public struct KedaTrigger: Decodable, Equatable, Identifiable, Sendable {
    public let type: String
    public let summary: String

    public var label: String { [type, summary].filter { !$0.isEmpty }.joined(separator: ": ") }
    public var id: String { label }

    public init(type: String = "", summary: String = "") {
        self.type = type
        self.summary = summary
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        summary = try c.field(.summary, "")
    }

    private enum CodingKeys: String, CodingKey { case type, summary }
}

/// One VerticalPodAutoscaler with its recommendation per container.
public struct VpaRow: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let target: AutoscaleTarget
    /// Off, Initial, Recreate, Auto or InPlaceOrRecreate.
    public let updateMode: String
    public let containers: [VpaContainer]
    public let recommendationProvided: Bool
    public let message: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(namespace: String = "", name: String = "", target: AutoscaleTarget = AutoscaleTarget(), updateMode: String = "",
                containers: [VpaContainer] = [], recommendationProvided: Bool = false, message: String = "", state: AutoscaleState = .ok) {
        self.namespace = namespace
        self.name = name
        self.target = target
        self.updateMode = updateMode
        self.containers = containers
        self.recommendationProvided = recommendationProvided
        self.message = message
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        target = try c.field(.target, AutoscaleTarget())
        updateMode = try c.field(.updateMode, "")
        containers = try c.field(.containers, [])
        recommendationProvided = try c.field(.recommendationProvided, false)
        message = try c.field(.message, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, target, updateMode, containers, recommendationProvided, message, state }
}

/// The recommender's answer for one container next to its live requests ("" when unread).
public struct VpaContainer: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let cpuTarget: String
    public let memoryTarget: String
    public let cpuLower: String
    public let cpuUpper: String
    public let memoryLower: String
    public let memoryUpper: String
    public let cpuRequest: String
    public let memoryRequest: String

    public var id: String { name }

    public init(name: String = "", cpuTarget: String = "", memoryTarget: String = "", cpuLower: String = "", cpuUpper: String = "",
                memoryLower: String = "", memoryUpper: String = "", cpuRequest: String = "", memoryRequest: String = "") {
        self.name = name
        self.cpuTarget = cpuTarget
        self.memoryTarget = memoryTarget
        self.cpuLower = cpuLower
        self.cpuUpper = cpuUpper
        self.memoryLower = memoryLower
        self.memoryUpper = memoryUpper
        self.cpuRequest = cpuRequest
        self.memoryRequest = memoryRequest
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        cpuTarget = try c.field(.cpuTarget, "")
        memoryTarget = try c.field(.memoryTarget, "")
        cpuLower = try c.field(.cpuLower, "")
        cpuUpper = try c.field(.cpuUpper, "")
        memoryLower = try c.field(.memoryLower, "")
        memoryUpper = try c.field(.memoryUpper, "")
        cpuRequest = try c.field(.cpuRequest, "")
        memoryRequest = try c.field(.memoryRequest, "")
    }

    private enum CodingKeys: String, CodingKey {
        case name, cpuTarget, memoryTarget, cpuLower, cpuUpper, memoryLower, memoryUpper, cpuRequest, memoryRequest
    }
}

/// One Karpenter NodePool: its limits against what it runs, and how it may disrupt nodes.
public struct NodePoolRow: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let limits: NodePoolLimits
    public let resources: NodePoolResources
    public let atLimit: Bool
    public let disruption: NodePoolDisruption
    public let weight: Int
    public let ready: Bool
    public let message: String
    public let capacityTypes: [String]

    public var id: String { name }
    /// The state the row shows: at its limit, or not ready.
    public var state: AutoscaleState { atLimit ? .atMax : (ready ? .ok : .notReady) }

    public init(name: String = "", limits: NodePoolLimits = NodePoolLimits(), resources: NodePoolResources = NodePoolResources(),
                atLimit: Bool = false, disruption: NodePoolDisruption = NodePoolDisruption(), weight: Int = 0, ready: Bool = false,
                message: String = "", capacityTypes: [String] = []) {
        self.name = name
        self.limits = limits
        self.resources = resources
        self.atLimit = atLimit
        self.disruption = disruption
        self.weight = weight
        self.ready = ready
        self.message = message
        self.capacityTypes = capacityTypes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        limits = try c.field(.limits, NodePoolLimits())
        resources = try c.field(.resources, NodePoolResources())
        atLimit = try c.field(.atLimit, false)
        disruption = try c.field(.disruption, NodePoolDisruption())
        weight = try c.field(.weight, 0)
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        capacityTypes = try c.field(.capacityTypes, [])
    }

    private enum CodingKeys: String, CodingKey { case name, limits, resources, atLimit, disruption, weight, ready, message, capacityTypes }
}

public struct NodePoolLimits: Decodable, Equatable, Sendable {
    public let cpu: String
    public let memory: String

    public init(cpu: String = "", memory: String = "") {
        self.cpu = cpu
        self.memory = memory
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cpu = try c.field(.cpu, "")
        memory = try c.field(.memory, "")
    }

    private enum CodingKeys: String, CodingKey { case cpu, memory }
}

public struct NodePoolResources: Decodable, Equatable, Sendable {
    public let cpu: String
    public let memory: String
    public let nodes: Int

    public init(cpu: String = "", memory: String = "", nodes: Int = 0) {
        self.cpu = cpu
        self.memory = memory
        self.nodes = nodes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cpu = try c.field(.cpu, "")
        memory = try c.field(.memory, "")
        nodes = try c.field(.nodes, 0)
    }

    private enum CodingKeys: String, CodingKey { case cpu, memory, nodes }
}

public struct NodePoolDisruption: Decodable, Equatable, Sendable {
    public let consolidationPolicy: String
    public let consolidateAfter: String
    public let budgets: [NodePoolBudget]

    public init(consolidationPolicy: String = "", consolidateAfter: String = "", budgets: [NodePoolBudget] = []) {
        self.consolidationPolicy = consolidationPolicy
        self.consolidateAfter = consolidateAfter
        self.budgets = budgets
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        consolidationPolicy = try c.field(.consolidationPolicy, "")
        consolidateAfter = try c.field(.consolidateAfter, "")
        budgets = try c.field(.budgets, [])
    }

    private enum CodingKeys: String, CodingKey { case consolidationPolicy, consolidateAfter, budgets }
}

public struct NodePoolBudget: Decodable, Equatable, Identifiable, Sendable {
    public let nodes: String
    public let schedule: String
    public let duration: String
    public let reasons: [String]

    public var id: String { [nodes, schedule, duration].joined(separator: "/") }

    public init(nodes: String = "", schedule: String = "", duration: String = "", reasons: [String] = []) {
        self.nodes = nodes
        self.schedule = schedule
        self.duration = duration
        self.reasons = reasons
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, "")
        schedule = try c.field(.schedule, "")
        duration = try c.field(.duration, "")
        reasons = try c.field(.reasons, [])
    }

    private enum CodingKeys: String, CodingKey { case nodes, schedule, duration, reasons }
}

/// One Karpenter NodeClaim: a node being launched, running, or on its way out.
public struct NodeClaimRow: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let pool: String
    public let nodeName: String
    public let instanceType: String
    public let capacityType: String
    public let launched: Bool
    public let registered: Bool
    public let initialized: Bool
    public let ready: Bool
    public let message: String
    /// Unix ms.
    public let created: Int64
    public let pending: Bool
    public let drifted: Bool
    public let disrupting: Bool

    public var id: String { name }
    /// The state the row shows: pending, disrupting, drifted, or not ready.
    public var state: AutoscaleState {
        if pending { return .notReady }
        if disrupting { return .paused }
        if drifted { return .limited }
        return ready ? .ok : .notReady
    }

    public init(name: String = "", pool: String = "", nodeName: String = "", instanceType: String = "", capacityType: String = "",
                launched: Bool = false, registered: Bool = false, initialized: Bool = false, ready: Bool = false, message: String = "",
                created: Int64 = 0, pending: Bool = false, drifted: Bool = false, disrupting: Bool = false) {
        self.name = name
        self.pool = pool
        self.nodeName = nodeName
        self.instanceType = instanceType
        self.capacityType = capacityType
        self.launched = launched
        self.registered = registered
        self.initialized = initialized
        self.ready = ready
        self.message = message
        self.created = created
        self.pending = pending
        self.drifted = drifted
        self.disrupting = disrupting
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        pool = try c.field(.pool, "")
        nodeName = try c.field(.nodeName, "")
        instanceType = try c.field(.instanceType, "")
        capacityType = try c.field(.capacityType, "")
        launched = try c.field(.launched, false)
        registered = try c.field(.registered, false)
        initialized = try c.field(.initialized, false)
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        created = try c.field(.created, 0)
        pending = try c.field(.pending, false)
        drifted = try c.field(.drifted, false)
        disrupting = try c.field(.disrupting, false)
    }

    private enum CodingKeys: String, CodingKey {
        case name, pool, nodeName, instanceType, capacityType, launched, registered, initialized, ready, message, created, pending, drifted, disrupting
    }
}

/// One (workload, autoscaler kind) pair: what the workload sheet names.
public struct AutoscaledWorkload: Decodable, Equatable, Identifiable, Sendable {
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
public struct AutoscalingProblem: Equatable, Identifiable, Sendable {
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

/// "2 of 1–10": where the replica count sits between the bounds.
public func hpaBoundsText(_ current: Int, _ min: Int, _ max: Int) -> String { "\(current) of \(min)–\(max)" }

/// "100m → 250m": the live request against what the recommender wants (the CAST AI style).
public func vpaChangeText(_ request: String, _ target: String) -> String {
    if request.isEmpty { return target }
    if target.isEmpty { return request }
    if request == target { return target }
    return "\(request) → \(target)"
}
