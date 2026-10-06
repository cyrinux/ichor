import Foundation

/// SMART / NVMe health of one disk (NodeDiskHealth). Values the drive does not report are nil.
public struct DiskHealthInfo: Decodable, Equatable, Identifiable, Sendable {
    public let device: String
    public let model: String
    public let serial: String
    /// nil when there is no SMART data for this disk.
    public let healthy: Bool?
    /// What the drive says about its state, "" when nothing.
    public let message: String
    public let temperatureC: Double?
    public let powerOnHours: Int64?
    /// Percentage of the rated endurance used.
    public let wearPercent: Double?
    public let criticalWarnings: [String]
    public let attributes: [LogField]

    public var id: String { device }

    public init(device: String, model: String = "", serial: String = "", healthy: Bool? = nil, message: String = "", temperatureC: Double? = nil,
                powerOnHours: Int64? = nil, wearPercent: Double? = nil, criticalWarnings: [String] = [], attributes: [LogField] = []) {
        self.device = device
        self.model = model
        self.serial = serial
        self.healthy = healthy
        self.message = message
        self.temperatureC = temperatureC
        self.powerOnHours = powerOnHours
        self.wearPercent = wearPercent
        self.criticalWarnings = criticalWarnings
        self.attributes = attributes
    }

    private enum CodingKeys: String, CodingKey {
        case device, model, serial, healthy, message, temperatureC, powerOnHours, wearPercent, criticalWarnings, attributes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        device = try c.field(.device, "")
        model = try c.field(.model, "")
        serial = try c.field(.serial, "")
        healthy = try c.decodeIfPresent(Bool.self, forKey: .healthy)
        message = try c.field(.message, "")
        temperatureC = try c.decodeIfPresent(Double.self, forKey: .temperatureC)
        // A float from some drives' JSON: keep the whole hours.
        powerOnHours = try c.decodeIfPresent(Double.self, forKey: .powerOnHours).map { Int64($0) }
        wearPercent = try c.decodeIfPresent(Double.self, forKey: .wearPercent)
        // A count (NVMe critical_warning bits) or the list of what they mean.
        if let list = try? c.decodeIfPresent([String].self, forKey: .criticalWarnings) {
            criticalWarnings = list
        } else if let count = try? c.decodeIfPresent(Int.self, forKey: .criticalWarnings), count > 0 {
            criticalWarnings = ["\(count)"]
        } else {
            criticalWarnings = []
        }
        attributes = try c.field(.attributes, [])
    }

    /// A disk with critical warnings is failing even if it still claims to be healthy.
    public var state: DiskHealthState {
        if healthy == false || !criticalWarnings.isEmpty { return .failing }
        return healthy == true ? .healthy : .unknown
    }
}

public enum DiskHealthState: Equatable, Sendable {
    case healthy, failing, unknown
}

public struct NodeDiskHealth: Decodable, Equatable, Sendable {
    /// False when the node's Talos cannot report disk health.
    public let supported: Bool
    /// Why there is no health: the Talos version when unsupported, SMART collection not
    /// configured when supported (the disks are then listed without a verdict). "" otherwise.
    public let reason: String
    public let disks: [DiskHealthInfo]

    public init(supported: Bool, reason: String = "", disks: [DiskHealthInfo] = []) {
        self.supported = supported
        self.reason = reason
        self.disks = disks
    }

    private enum CodingKeys: String, CodingKey { case supported, reason, disks }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        supported = try c.field(.supported, true)
        reason = try c.field(.reason, "")
        disks = try c.field(.disks, [])
    }
}

/// Failing disks first, then by device name.
public func sortDiskHealth(_ disks: [DiskHealthInfo]) -> [DiskHealthInfo] {
    func rank(_ state: DiskHealthState) -> Int {
        switch state {
        case .failing: 0
        case .unknown: 1
        case .healthy: 2
        }
    }
    return disks.sorted { a, b in
        let ra = rank(a.state), rb = rank(b.state)
        if ra != rb { return ra < rb }
        return a.device < b.device
    }
}

/// Wear follows the usage thresholds: orange from 80 %, red from 90 %.
public func wearLevel(_ percent: Double?) -> UsageLevel {
    percent.map(usageLevel(percent:)) ?? .normal
}

/// "41 °C"; whole degrees unless the drive reports a fraction.
public func formatTemperature(_ celsius: Double) -> String {
    celsius == celsius.rounded() ? "\(Int(celsius)) °C" : String(format: "%.1f °C", celsius)
}
