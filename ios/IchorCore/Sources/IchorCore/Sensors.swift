import Foundation

/// A node's hardware sensors (Go NodeSensors): hwmon temperatures, fans and voltages,
/// thermal zones, per-CPU frequency, thermal throttle counters and PCI devices. A VM has
/// every section empty; a failed section is named in `errors`
/// (hwmon, thermal, throttle, cpuFreq, pci).
public struct NodeSensors: Decodable, Equatable, Sendable {
    public let temperatures: [TemperatureSensor]
    public let fans: [FanSensor]
    public let voltages: [VoltageSensor]
    public let thermalZones: [ThermalZone]
    public let cpuFreq: [CPUFrequency]
    /// Nil when the CPU has no throttle counters (AMD, most VMs).
    public let throttle: ThrottleCounts?
    /// A core runs below 70 % of its max under the performance governor, or a sensor is
    /// above its max.
    public let throttled: Bool
    public let pci: [PCIDevice]
    public let errors: [String: String]

    public init(temperatures: [TemperatureSensor] = [], fans: [FanSensor] = [], voltages: [VoltageSensor] = [],
                thermalZones: [ThermalZone] = [], cpuFreq: [CPUFrequency] = [], throttle: ThrottleCounts? = nil,
                throttled: Bool = false, pci: [PCIDevice] = [], errors: [String: String] = [:]) {
        self.temperatures = temperatures
        self.fans = fans
        self.voltages = voltages
        self.thermalZones = thermalZones
        self.cpuFreq = cpuFreq
        self.throttle = throttle
        self.throttled = throttled
        self.pci = pci
        self.errors = errors
    }

    private enum CodingKeys: String, CodingKey {
        case temperatures, fans, voltages, thermalZones, cpuFreq, throttle, throttled, pci, errors
    }

    // Go encodes empty (nil) slices and maps as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        temperatures = try c.field(.temperatures, [])
        fans = try c.field(.fans, [])
        voltages = try c.field(.voltages, [])
        thermalZones = try c.field(.thermalZones, [])
        cpuFreq = try c.field(.cpuFreq, [])
        throttle = try c.decodeIfPresent(ThrottleCounts.self, forKey: .throttle)
        throttled = try c.field(.throttled, false)
        pci = try c.field(.pci, [])
        errors = try c.field(.errors, [:])
    }

    /// Nothing for the Sensors section: a VM.
    public var sensorsEmpty: Bool {
        temperatures.isEmpty && fans.isEmpty && voltages.isEmpty && thermalZones.isEmpty && cpuFreq.isEmpty
    }

    /// The Sensors section's first error, if one of its parts failed (PCI has its own).
    public var sensorsError: String? {
        ["hwmon", "thermal", "cpuFreq", "throttle"].lazy.compactMap { errors[$0] }.first
    }
}

public struct TemperatureSensor: Decodable, Equatable, Sendable, Identifiable {
    public let chip: String
    public let label: String
    public let celsius: Double
    /// 0 when the sensor has none.
    public let critCelsius: Double
    /// 0 when the sensor has none.
    public let maxCelsius: Double

    public var id: String { "\(chip)/\(label)" }

    public init(chip: String = "", label: String = "", celsius: Double = 0, critCelsius: Double = 0, maxCelsius: Double = 0) {
        self.chip = chip
        self.label = label
        self.celsius = celsius
        self.critCelsius = critCelsius
        self.maxCelsius = maxCelsius
    }

    /// How far the temperature is towards its limit (max, else crit), 0...1; nil without one.
    public var limitFraction: Double? {
        let limit = maxCelsius > 0 ? maxCelsius : critCelsius
        guard limit > 0 else { return nil }
        return min(max(celsius / limit, 0), 1)
    }

    /// Above its max, or at its critical temperature.
    public var hot: Bool {
        (maxCelsius > 0 && celsius > maxCelsius) || (critCelsius > 0 && celsius >= critCelsius)
    }
}

public struct FanSensor: Decodable, Equatable, Sendable, Identifiable {
    public let chip: String
    public let label: String
    public let rpm: Int64

    public var id: String { "\(chip)/\(label)" }

    public init(chip: String = "", label: String = "", rpm: Int64 = 0) {
        self.chip = chip
        self.label = label
        self.rpm = rpm
    }
}

public struct VoltageSensor: Decodable, Equatable, Sendable, Identifiable {
    public let chip: String
    public let label: String
    public let volts: Double

    public var id: String { "\(chip)/\(label)" }

    public init(chip: String = "", label: String = "", volts: Double = 0) {
        self.chip = chip
        self.label = label
        self.volts = volts
    }
}

public struct ThermalZone: Decodable, Equatable, Sendable {
    public let type: String
    public let celsius: Double

    public init(type: String = "", celsius: Double = 0) {
        self.type = type
        self.celsius = celsius
    }
}

public struct CPUFrequency: Decodable, Equatable, Sendable, Identifiable {
    public let cpu: Int
    public let currentMhz: Int64
    public let minMhz: Int64
    public let maxMhz: Int64
    public let governor: String

    public var id: Int { cpu }

    public init(cpu: Int = 0, currentMhz: Int64 = 0, minMhz: Int64 = 0, maxMhz: Int64 = 0, governor: String = "") {
        self.cpu = cpu
        self.currentMhz = currentMhz
        self.minMhz = minMhz
        self.maxMhz = maxMhz
        self.governor = governor
    }
}

public struct ThrottleCounts: Decodable, Equatable, Sendable {
    public let coreEvents: Int64
    public let packageEvents: Int64

    public init(coreEvents: Int64 = 0, packageEvents: Int64 = 0) {
        self.coreEvents = coreEvents
        self.packageEvents = packageEvents
    }
}

public struct PCIDevice: Decodable, Equatable, Sendable, Identifiable {
    public let id: String
    public let `class`: String
    public let subclass: String
    public let vendor: String
    public let product: String
    public let driver: String

    public init(id: String = "", class: String = "", subclass: String = "", vendor: String = "", product: String = "", driver: String = "") {
        self.id = id
        self.class = `class`
        self.subclass = subclass
        self.vendor = vendor
        self.product = product
        self.driver = driver
    }
}
