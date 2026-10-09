import Foundation

// Mirrors go/ichorgo/kube_castai.go (and the Android model/DataServicesCastAI.kt); the wire format
// is documented in plans/data-services/README.md.

/// CAST AI's Workload Autoscaler: one recommendation per workload it manages, and its node
/// consolidations (see CastAIPlans.swift).
public struct CastAIStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let recommendations: [CastAIRecommendation]
    /// Recommended minus original requests over every compared workload; negative is a saving.
    public let cpuDeltaMilli: Int64
    public let memoryDeltaBytes: Int64
    /// Workloads whose original requests are known.
    public let compared: Int
    /// Node consolidations (RebalancePlan), newest first.
    public let plans: [CastAIPlan]
    /// Nodes a plan failed to remove that a later plan tried again.
    public let stuck: [CastAIStuckNode]
    /// Why the plans could not be read; `error` is the recommendations' only.
    public let plansError: String

    public init(version: String = "", error: String = "", recommendations: [CastAIRecommendation] = [], cpuDeltaMilli: Int64 = 0,
                memoryDeltaBytes: Int64 = 0, compared: Int = 0, plans: [CastAIPlan] = [], stuck: [CastAIStuckNode] = [],
                plansError: String = "") {
        self.version = version
        self.error = error
        self.recommendations = recommendations
        self.cpuDeltaMilli = cpuDeltaMilli
        self.memoryDeltaBytes = memoryDeltaBytes
        self.compared = compared
        self.plans = plans
        self.stuck = stuck
        self.plansError = plansError
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        recommendations = try c.field(.recommendations, [])
        cpuDeltaMilli = try c.field(.cpuDeltaMilli, 0)
        memoryDeltaBytes = try c.field(.memoryDeltaBytes, 0)
        compared = try c.field(.compared, 0)
        plans = try c.field(.plans, [])
        stuck = try c.field(.stuck, [])
        plansError = try c.field(.plansError, "")
    }

    /// A copy with other recommendations (tests, filters).
    public func with(recommendations: [CastAIRecommendation]) -> CastAIStatus {
        CastAIStatus(version: version, error: error, recommendations: recommendations, cpuDeltaMilli: cpuDeltaMilli,
                     memoryDeltaBytes: memoryDeltaBytes, compared: compared, plans: plans, stuck: stuck, plansError: plansError)
    }

    /// Recommendations are the items; one the autoscaler cannot apply or is told not to needs a look.
    /// A node the consolidation keeps failing to remove is no recommendation: it only makes the
    /// system a warning (the card names the stuck nodes).
    public var summary: ServiceSummary {
        if !error.isEmpty && recommendations.isEmpty && plans.isEmpty {
            return ServiceSummary(total: 0, attention: 0, health: .unknown, error: error)
        }
        let healths = recommendations.map(\.health)
        let health = ServiceHealth.worst(healths + (stuck.isEmpty ? [] : [.warning]))
        return ServiceSummary(total: recommendations.count, attention: healths.filter(\.needsAttention).count, health: health, error: error)
    }

    private enum CodingKeys: String, CodingKey {
        case version, error, recommendations, cpuDeltaMilli, memoryDeltaBytes, compared, plans, stuck, plansError
    }
}

public struct CastAIRecommendation: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// The target workload's kind (Deployment, StatefulSet, CronJob) and name.
    public let kind: String
    public let workload: String
    /// How CAST AI applies it; unknown when the core does not say.
    public let mode: CastAIMode
    public let readOnly: Bool
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [CastAIReason]
    /// The failing condition's message or the read-only reason, in CAST AI's words.
    public let message: String
    public let containers: [CastAIContainer]
    public let cpuDeltaMilli: Int64
    public let memoryDeltaBytes: Int64
    /// A pod's requests as recommended and before CAST AI (an unknown original counts as recommended).
    public let cpuMilli: Int64
    public let memoryBytes: Int64
    public let originalCpuMilli: Int64
    public let originalMemoryBytes: Int64

    /// The recommendation object's own name: unique in its namespace.
    public var id: String { "\(namespace)/\(name)" }
    public var label: String { "\(namespace)/\(workload)" }
    /// The workload's name, the recommendation's when unknown.
    public var title: String { workload.isEmpty ? name : workload }
    /// "shop · Deployment".
    public var subtitle: String { [namespace, kind].filter { !$0.isEmpty }.joined(separator: " · ") }

    public init(namespace: String = "", name: String = "", kind: String = "", workload: String = "", mode: CastAIMode = .unknown,
                readOnly: Bool = false, health: ServiceHealth = .unknown, reasons: [CastAIReason] = [], message: String = "",
                containers: [CastAIContainer] = [], cpuDeltaMilli: Int64 = 0, memoryDeltaBytes: Int64 = 0, cpuMilli: Int64 = 0,
                memoryBytes: Int64 = 0, originalCpuMilli: Int64 = 0, originalMemoryBytes: Int64 = 0) {
        self.namespace = namespace
        self.name = name
        self.kind = kind
        self.workload = workload
        self.mode = mode
        self.readOnly = readOnly
        self.health = health
        self.reasons = reasons
        self.message = message
        self.containers = containers
        self.cpuDeltaMilli = cpuDeltaMilli
        self.memoryDeltaBytes = memoryDeltaBytes
        self.cpuMilli = cpuMilli
        self.memoryBytes = memoryBytes
        self.originalCpuMilli = originalCpuMilli
        self.originalMemoryBytes = originalMemoryBytes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        kind = try c.field(.kind, "")
        workload = try c.field(.workload, "")
        mode = try c.wire(.mode)
        readOnly = try c.field(.readOnly, false)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        message = try c.field(.message, "")
        containers = try c.field(.containers, [])
        cpuDeltaMilli = try c.field(.cpuDeltaMilli, 0)
        memoryDeltaBytes = try c.field(.memoryDeltaBytes, 0)
        cpuMilli = try c.field(.cpuMilli, 0)
        memoryBytes = try c.field(.memoryBytes, 0)
        originalCpuMilli = try c.field(.originalCpuMilli, 0)
        originalMemoryBytes = try c.field(.originalMemoryBytes, 0)
    }

    /// Whether the requests grow or shrink; unknown without the originals.
    public var change: CastAIChange {
        if !containers.contains(where: { !$0.originalCpu.isEmpty || !$0.originalMemory.isEmpty }) { return .unknown }
        if cpuDeltaMilli > 0 || memoryDeltaBytes > 0 { return .grow }
        if cpuDeltaMilli < 0 || memoryDeltaBytes < 0 { return .shrink }
        return .same
    }

    /// The highest memory request as a share of its limit, 0 without limits.
    public var memoryLimitPercent: Int { containers.map(\.memoryLimitPercent).max() ?? 0 }

    public var nearMemoryLimit: Bool { containers.contains(where: \.nearMemoryLimit) }

    /// How much the recommendation moves, one core weighing as much as 4 GiB (cloud pricing).
    public var weight: Double {
        let gib = 1024.0 * 1024 * 1024
        return Double(abs(cpuDeltaMilli)) / 1000 + Double(abs(memoryDeltaBytes)) / gib / 4
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, kind, workload, mode, readOnly, health, reasons, message, containers
        case cpuDeltaMilli, memoryDeltaBytes, cpuMilli, memoryBytes, originalCpuMilli, originalMemoryBytes
    }
}

/// Which way a recommendation moves a workload's requests.
public enum CastAIChange: Hashable, Sendable {
    case shrink, grow, same, unknown
}

/// One container's recommended requests and limits, with the requests CAST AI first saw ("" unknown).
public struct CastAIContainer: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let cpu: String
    public let memory: String
    public let cpuLimit: String
    public let memoryLimit: String
    public let originalCpu: String
    public let originalMemory: String
    /// The recommended request as a share of its limit, 0 without a limit.
    public let cpuLimitPercent: Int
    public let memoryLimitPercent: Int

    public var id: String { name }

    /// CAST AI keeps limits, so a request this close to its memory limit risks an OOM kill.
    public var nearMemoryLimit: Bool { memoryLimitPercent >= castAINearLimitPercent }

    public init(name: String = "", cpu: String = "", memory: String = "", cpuLimit: String = "", memoryLimit: String = "",
                originalCpu: String = "", originalMemory: String = "", cpuLimitPercent: Int = 0, memoryLimitPercent: Int = 0) {
        self.name = name
        self.cpu = cpu
        self.memory = memory
        self.cpuLimit = cpuLimit
        self.memoryLimit = memoryLimit
        self.originalCpu = originalCpu
        self.originalMemory = originalMemory
        self.cpuLimitPercent = cpuLimitPercent
        self.memoryLimitPercent = memoryLimitPercent
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        cpu = try c.field(.cpu, "")
        memory = try c.field(.memory, "")
        cpuLimit = try c.field(.cpuLimit, "")
        memoryLimit = try c.field(.memoryLimit, "")
        originalCpu = try c.field(.originalCpu, "")
        originalMemory = try c.field(.originalMemory, "")
        cpuLimitPercent = try c.field(.cpuLimitPercent, 0)
        memoryLimitPercent = try c.field(.memoryLimitPercent, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case name, cpu, memory, cpuLimit, memoryLimit, originalCpu, originalMemory, cpuLimitPercent, memoryLimitPercent
    }
}

public let castAINearLimitPercent = 85

/// Why a recommendation needs a look, as the Go core names it.
public enum CastAIReason: String, Sendable {
    case vpa, hpa, readOnly
}

/// How CAST AI applies a recommendation.
public enum CastAIMode: String, Hashable, Sendable, WireEnum {
    case immediate, deferred, unknown = ""

    public static var wireFallback: CastAIMode { .unknown }
}

/// Millicores as Kubernetes writes them, signed: -380 is "-380m", 2000 is "2".
public func formatMilliCores(_ milli: Int64, signed: Bool = false) -> String {
    let sign = milli < 0 ? "-" : (signed && milli > 0 ? "+" : "")
    let value = abs(milli)
    return value % 1000 == 0 ? "\(sign)\(value / 1000)" : "\(sign)\(value)m"
}

/// "+384.0 MiB", "-1.2 GiB", "0 B".
public func signedBytes(_ bytes: Int64) -> String {
    if bytes < 0 { return "-" + formatBytes(-bytes) }
    if bytes > 0 { return "+" + formatBytes(bytes) }
    return formatBytes(0)
}

/// "500m → 120m", or the one value when it does not change.
public func castAIChangeText(_ before: String, _ after: String) -> String {
    before == after ? after : "\(before) → \(after)"
}

/// "≈ $120" for an estimated amount.
public func castAIApprox(_ amount: String, estimated: Bool) -> String {
    estimated ? "≈ \(amount)" : amount
}

/// `amount` of `currency` in the user's locale ("$418", "418 €"); the bare number without a currency.
public func formatMoney(_ amount: Double, currency: String, decimals: Int = 0, locale: Locale = .current) -> String {
    guard currency.count == 3 else { return String(format: "%.\(decimals)f", amount) }
    let formatter = NumberFormatter()
    formatter.locale = locale
    formatter.numberStyle = .currency
    formatter.currencyCode = currency
    formatter.minimumFractionDigits = decimals
    formatter.maximumFractionDigits = decimals
    return formatter.string(from: NSNumber(value: amount)) ?? String(format: "%.\(decimals)f", amount)
}
