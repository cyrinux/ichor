package name.levis.ichor.monitor

import name.levis.ichor.model.ClusterSensorHealth
import name.levis.ichor.model.TemperatureSensor
import java.util.Locale

/** The temperature threshold: 0 is automatic (the sensor's crit − [SENSOR_MARGIN], else its max), else °C. */
const val SENSOR_THRESHOLD_AUTO = 0
val SENSOR_THRESHOLD_RANGE = 60..100

/** The threshold a fixed temperature starts at. */
const val SENSOR_THRESHOLD_DEFAULT = 85

/** How far under a sensor's crit the automatic threshold is, and how far under it a hot node must cool to clear. */
const val SENSOR_MARGIN = 5.0
const val SENSOR_HYSTERESIS = 3.0

private const val KIND_HOT = "hot"
private const val KIND_THROTTLED = "throttled"

/**
 * A sensor issue's stored value, split: "severity|hot|hostname|label|celsius|limit" for a node
 * running hot, "severity|throttled|hostname||0|0" for a throttled one.
 */
data class SensorDetail(
    val severity: String,
    val throttled: Boolean,
    val hostname: String,
    val label: String = "",
    val celsius: Double = 0.0,
    val limit: Double = 0.0,
) {
    fun format(): String = listOf(
        severity, if (throttled) KIND_THROTTLED else KIND_HOT, hostname.clean(), label.clean(), celsius.oneDecimal(), limit.oneDecimal(),
    ).joinToString("|")

    companion object {
        fun parse(value: String): SensorDetail {
            val parts = value.split('|', limit = 6)
            fun part(i: Int) = parts.getOrElse(i) { "" }
            return SensorDetail(
                severity = part(0),
                throttled = part(1) == KIND_THROTTLED,
                hostname = part(2),
                label = part(3),
                celsius = part(4).toDoubleOrNull() ?: 0.0,
                limit = part(5).toDoubleOrNull() ?: 0.0,
            )
        }
    }
}

/** The tracked severity of a stored sensor value: running hot alerts at once, throttling on its second check. */
fun sensorSeverity(value: String): String = if (SensorDetail.parse(value).severity == DATA_CRITICAL) DATA_CRITICAL else DATA_WARNING

/** The temperature [sensor] alerts at: [threshold] when set, else crit − margin, else max; null without either. */
fun sensorLimit(sensor: TemperatureSensor, threshold: Int): Double? = when {
    threshold != SENSOR_THRESHOLD_AUTO -> threshold.coerceIn(SENSOR_THRESHOLD_RANGE).toDouble()
    sensor.critCelsius > 0 -> sensor.critCelsius - SENSOR_MARGIN
    sensor.maxCelsius > 0 -> sensor.maxCelsius
    else -> null
}

/**
 * The sensor issues of [health], keyed "node|hot" and "node|throttled": a node whose hottest
 * sensor reaches its limit (see [sensorLimit]) runs hot, and stays so until it is
 * [SENSOR_HYSTERESIS] °C under it ([known] says it was); a throttled node is a warning, which the
 * monitor only alerts on its second check in a row. A node that did not answer keeps its [known]
 * issues; a node without sensors (a VM) has none.
 */
fun sensorIssuesOf(health: ClusterSensorHealth, threshold: Int, known: Map<String, String> = emptyMap()): Map<String, String> {
    val out = sortedMapOf<String, String>()
    health.nodes.forEach { node ->
        if (node.error != null) {
            out += known.filterKeys { it.substringBefore('|') == node.node }
            return@forEach
        }
        val host = node.hostname.ifBlank { node.node }
        node.hottest?.let { sensor ->
            val limit = sensorLimit(sensor, threshold) ?: return@let
            val hotKey = "${node.node}|$KIND_HOT"
            val wasHot = hotKey in known
            if (sensor.celsius >= limit || (wasHot && sensor.celsius > limit - SENSOR_HYSTERESIS)) {
                out[hotKey] = SensorDetail(DATA_CRITICAL, throttled = false, host, sensor.label, sensor.celsius, limit).format()
            }
        }
        if (node.throttled) {
            out["${node.node}|$KIND_THROTTLED"] = SensorDetail(DATA_WARNING, throttled = true, host).format()
        }
    }
    return out
}

/** The sensor issues kept by [previous] when it is the same cluster ([context]) and that check read them. */
fun knownSensorIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.sensorWatched && it.sensorChecked }?.sensorIssues.orEmpty()

private fun Double.oneDecimal(): String = String.format(Locale.ROOT, "%.1f", this)

/** Without the separator, so a value always splits back the same way. */
private fun String.clean(): String = replace('|', '/')
