package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/sensors.go.

@Serializable
data class NodeSensors(
    val temperatures: List<TemperatureSensor> = emptyList(),
    val fans: List<FanSensor> = emptyList(),
    val voltages: List<VoltageSensor> = emptyList(),
    val thermalZones: List<ThermalZone> = emptyList(),
    val cpuFreq: List<CpuFrequency> = emptyList(),
    /** Null when the CPU has no throttle counters (AMD, most VMs). */
    val throttle: ThrottleCounts? = null,
    /** A core runs below 70 % of its max under the performance governor, or a sensor is above its max. */
    val throttled: Boolean = false,
    val pci: List<PciDevice> = emptyList(),
    /** Section ("hwmon", "thermal", "throttle", "cpuFreq", "pci") -> error. */
    val errors: Map<String, String> = emptyMap(),
)

@Serializable
data class TemperatureSensor(
    val chip: String = "",
    val label: String = "",
    val celsius: Double = 0.0,
    /** 0 when the sensor has none. */
    val critCelsius: Double = 0.0,
    /** 0 when the sensor has none. */
    val maxCelsius: Double = 0.0,
)

@Serializable
data class FanSensor(val chip: String = "", val label: String = "", val rpm: Long = 0)

@Serializable
data class VoltageSensor(val chip: String = "", val label: String = "", val volts: Double = 0.0)

@Serializable
data class ThermalZone(val type: String = "", val celsius: Double = 0.0)

@Serializable
data class CpuFrequency(
    val cpu: Int = 0,
    val currentMhz: Long = 0,
    val minMhz: Long = 0,
    val maxMhz: Long = 0,
    val governor: String = "",
)

@Serializable
data class ThrottleCounts(val coreEvents: Long = 0, val packageEvents: Long = 0)

@Serializable
data class PciDevice(
    val id: String = "",
    val `class`: String = "",
    val subclass: String = "",
    val vendor: String = "",
    val product: String = "",
    val driver: String = "",
)

object SensorSection {
    const val HWMON = "hwmon"
    const val THERMAL = "thermal"
    const val THROTTLE = "throttle"
    const val CPU_FREQ = "cpuFreq"
    const val PCI = "pci"
}

/** Nothing to show in the Sensors card: a VM. */
val NodeSensors.sensorsEmpty: Boolean
    get() = temperatures.isEmpty() && fans.isEmpty() && voltages.isEmpty() && thermalZones.isEmpty() && cpuFreq.isEmpty()

/** The Sensors card's first error, if any of its sections failed. */
val NodeSensors.sensorsError: String?
    get() = listOf(SensorSection.HWMON, SensorSection.THERMAL, SensorSection.CPU_FREQ, SensorSection.THROTTLE)
        .firstNotNullOfOrNull { errors[it] }

/** How far a temperature is towards its limit (max, else crit), 0..1; null without a limit. */
val TemperatureSensor.limitFraction: Float?
    get() {
        val limit = if (maxCelsius > 0) maxCelsius else critCelsius
        if (limit <= 0) return null
        return (celsius / limit).toFloat().coerceIn(0f, 1f)
    }

/** Above its max (or crit when it has no max). */
val TemperatureSensor.hot: Boolean
    get() = (maxCelsius > 0 && celsius > maxCelsius) || (critCelsius > 0 && celsius >= critCelsius)
