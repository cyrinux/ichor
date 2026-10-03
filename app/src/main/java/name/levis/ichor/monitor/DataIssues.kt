package name.levis.ichor.monitor

import name.levis.ichor.model.CnpgReason
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.GarageState
import name.levis.ichor.model.ServiceHealth

/** Severities of a data-service issue in a snapshot. */
const val DATA_CRITICAL = "critical"
const val DATA_WARNING = "warning"

/** Postgres reasons worth waking someone for; a switchover or a missing replica is usually planned. */
private val CNPG_ALERT_REASONS = setOf(CnpgReason.ARCHIVING, CnpgReason.BACKUP_FAILED, CnpgReason.BACKUP_STALE)

/**
 * The problems of [services] worth a notification, keyed "system|label" (e.g. "longhorn|db/data")
 * with their severity: a faulted volume, an unavailable Garage cluster or a Postgres cluster without
 * any instance are critical; a degraded volume, a degraded Garage cluster (or blocks failing to
 * resync) and failing Postgres backups or archiving are warnings.
 */
fun dataIssuesOf(services: DataServices): Map<String, String> {
    val out = sortedMapOf<String, String>()
    services.longhorn?.volumes.orEmpty().forEach { v ->
        when (v.serviceHealth) {
            ServiceHealth.CRITICAL -> out["longhorn|${v.label}"] = DATA_CRITICAL
            ServiceHealth.WARNING -> out["longhorn|${v.label}"] = DATA_WARNING
            else -> Unit
        }
    }
    services.garage?.instances.orEmpty().forEach { g ->
        when {
            g.state == GarageState.UNAVAILABLE -> out["garage|${g.label}"] = DATA_CRITICAL
            g.state == GarageState.DEGRADED || g.resyncErrors > 0 -> out["garage|${g.label}"] = DATA_WARNING
        }
    }
    services.cnpg?.clusters.orEmpty().forEach { c ->
        when {
            c.serviceHealth == ServiceHealth.CRITICAL -> out["cnpg|${c.label}"] = DATA_CRITICAL
            c.reasonList.any { it in CNPG_ALERT_REASONS } -> out["cnpg|${c.label}"] = DATA_WARNING
        }
    }
    return out
}
