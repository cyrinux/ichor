package name.levis.ichor.monitor

import name.levis.ichor.model.ClusterStorageHealth
import name.levis.ichor.model.StorageDiskHealth
import kotlin.math.floor

/** Volume fill thresholds (% used): the defaults, and the ranges offered (critical above warning). */
const val STORAGE_WARN_DEFAULT = 85
const val STORAGE_CRITICAL_DEFAULT = 95
val STORAGE_WARN_RANGE = 50..98
const val STORAGE_CRITICAL_MAX = 99

/** The critical thresholds a [warn] threshold allows. */
fun storageCriticalRange(warn: Int): IntRange = (warn.coerceIn(STORAGE_WARN_RANGE) + 1)..STORAGE_CRITICAL_MAX

private const val KIND_FILL = "fill"
private const val KIND_SMART = "smart"

/**
 * A storage issue's stored value, split: "severity|fill|hostname|volume|percent|free|size|warn|"
 * for a volume filling up, "severity|smart|hostname|device|0|0|0|warn|reason" for a disk failing SMART.
 * [percent] is the whole percent used (floored, so it never reads above a threshold it is under);
 * [warn] the warning threshold the check used, for the "back under" text.
 */
data class StorageDetail(
    val severity: String,
    val smart: Boolean,
    val hostname: String,
    val name: String,
    val percent: Int = 0,
    val freeBytes: Long = 0,
    val sizeBytes: Long = 0,
    val warn: Int = STORAGE_WARN_DEFAULT,
    val reason: String = "",
) {
    fun format(): String = listOf(
        severity, if (smart) KIND_SMART else KIND_FILL, hostname.clean(), name.clean(),
        percent.toString(), freeBytes.toString(), sizeBytes.toString(), warn.toString(), reason.clean(),
    ).joinToString("|")

    companion object {
        fun parse(value: String): StorageDetail {
            val parts = value.split('|', limit = 9)
            fun part(i: Int) = parts.getOrElse(i) { "" }
            return StorageDetail(
                severity = part(0),
                smart = part(1) == KIND_SMART,
                hostname = part(2),
                name = part(3),
                percent = part(4).toIntOrNull() ?: 0,
                freeBytes = part(5).toLongOrNull() ?: 0,
                sizeBytes = part(6).toLongOrNull() ?: 0,
                warn = part(7).toIntOrNull() ?: STORAGE_WARN_DEFAULT,
                reason = part(8),
            )
        }
    }
}

/** The tracked severity of a stored storage value. */
fun storageSeverity(value: String): String = if (StorageDetail.parse(value).severity == DATA_CRITICAL) DATA_CRITICAL else DATA_WARNING

/**
 * The storage issues of [health], keyed as the Go core keys them ("node|volume", "node|smart|device"):
 * a volume used at [warn] % or more is a warning, at [critical] % or more critical; a disk failing
 * SMART is critical. A node that did not answer keeps its [known] issues: unreadable, not resolved.
 */
fun storageIssuesOf(
    health: ClusterStorageHealth,
    warn: Int,
    critical: Int,
    known: Map<String, String> = emptyMap(),
): Map<String, String> {
    val warnAt = warn.coerceIn(STORAGE_WARN_RANGE)
    val criticalAt = critical.coerceIn(storageCriticalRange(warnAt))
    val out = sortedMapOf<String, String>()
    health.nodes.forEach { node ->
        if (node.error != null) {
            out += known.filterKeys { it.substringBefore('|') == node.node }
            return@forEach
        }
        val host = node.hostname.ifBlank { node.node }
        node.volumes.forEach { volume ->
            val severity = when {
                volume.usedPercent >= criticalAt -> DATA_CRITICAL
                volume.usedPercent >= warnAt -> DATA_WARNING
                else -> return@forEach
            }
            out[volume.key] = StorageDetail(
                severity, smart = false, host, volume.name, floor(volume.usedPercent).toInt(),
                volume.freeBytes, volume.sizeBytes, warnAt,
            ).format()
        }
        node.disks.filter { it.health == StorageDiskHealth.SMART_FAILING }.forEach { disk ->
            out[disk.key] = StorageDetail(
                DATA_CRITICAL, smart = true, host, disk.device, reason = disk.reason.ifBlank { disk.model },
            ).format()
        }
    }
    return out
}

/**
 * The storage issues a node that did not answer may carry over: [previous]'s, only when it is the
 * same cluster ([context]) and that check watched and read them.
 */
fun knownStorageIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.storageWatched && it.storageChecked }?.storageIssues.orEmpty()

/** Without the separator, so a value always splits back the same way. */
private fun String.clean(): String = replace('|', '/')
