import Foundation

// Mirrors go/ichorgo/hardware_cpu.go: Talos 1.15's NUMATopology and CPUScalingStatus.

/// One NUMA node of the host: its CPUs as ranges ("0-15,32-47") and its memory.
public struct NUMANode: Decodable, Equatable, Identifiable, Sendable {
    public let id: Int
    public let cpus: String
    public let cpuCount: Int
    public let memoryBytes: Int64

    public init(id: Int, cpus: String = "", cpuCount: Int = 0, memoryBytes: Int64 = 0) {
        self.id = id
        self.cpus = cpus
        self.cpuCount = cpuCount
        self.memoryBytes = memoryBytes
    }

    private enum CodingKeys: String, CodingKey { case id, cpus, cpuCount, memoryBytes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, 0)
        cpus = try c.field(.cpus, "")
        cpuCount = try c.field(.cpuCount, 0)
        memoryBytes = try c.field(.memoryBytes, 0)
    }
}

/// One cpufreq policy: the CPUs it drives, its governor and the frequency window it allows.
public struct CPUScalingPolicy: Decodable, Equatable, Identifiable, Sendable {
    public let policy: String
    public let cpus: String
    public let driver: String
    public let governor: String
    /// "" unless the driver has energy performance preferences.
    public let energyPreference: String
    public let minKhz: Int64
    public let maxKhz: Int64
    /// performance or efficiency on hybrid CPUs, "" otherwise.
    public let coreType: String
    /// The CPUScalingConfig document that set the policy, "" when none did.
    public let configuredBy: String

    public var id: String { policy }

    public init(
        policy: String, cpus: String = "", driver: String = "", governor: String = "",
        energyPreference: String = "", minKhz: Int64 = 0, maxKhz: Int64 = 0, coreType: String = "",
        configuredBy: String = ""
    ) {
        self.policy = policy
        self.cpus = cpus
        self.driver = driver
        self.governor = governor
        self.energyPreference = energyPreference
        self.minKhz = minKhz
        self.maxKhz = maxKhz
        self.coreType = coreType
        self.configuredBy = configuredBy
    }

    private enum CodingKeys: String, CodingKey {
        case policy, cpus, driver, governor, energyPreference, minKhz, maxKhz, coreType, configuredBy
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        policy = try c.field(.policy, "")
        cpus = try c.field(.cpus, "")
        driver = try c.field(.driver, "")
        governor = try c.field(.governor, "")
        energyPreference = try c.field(.energyPreference, "")
        minKhz = try c.field(.minKhz, 0)
        maxKhz = try c.field(.maxKhz, 0)
        coreType = try c.field(.coreType, "")
        configuredBy = try c.field(.configuredBy, "")
    }

    /// The powersave governor, the classic cause of a slow node.
    public var isPowersave: Bool { governor == "powersave" }
}

extension NodeHardware {
    /// The NUMA section shows only on a machine split in more than one node (or when its read failed).
    public var showsNUMA: Bool { numa.count > 1 || errors["numa"] != nil }

    /// The CPU scaling section: policies to show, or a read that failed. Absent before Talos 1.15 and on VMs.
    public var showsCPUScaling: Bool { !cpuScaling.isEmpty || errors["cpuScaling"] != nil }

    /// A powersave policy on a node that is throttled: the likely reason it runs slow.
    public func powersaveWhileThrottled(_ throttled: Bool) -> Bool {
        throttled && cpuScaling.contains(where: \.isPowersave)
    }
}
