package name.levis.ichor.monitor

import name.levis.ichor.model.CephReason
import name.levis.ichor.model.CertReason
import name.levis.ichor.model.CnpgReason
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.DragonflyReason
import name.levis.ichor.model.GarageState
import name.levis.ichor.model.MariaDbReason
import name.levis.ichor.model.PerconaReason
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.VeleroReason

/** Severities of a data-service issue in a snapshot. */
const val DATA_CRITICAL = "critical"
const val DATA_WARNING = "warning"

/** Ceph warnings worth a notification; a mon down or a reconcile in progress shows in Ceph's health anyway. */
private val CEPH_ALERT_REASONS = setOf(CephReason.HEALTH_WARN, CephReason.NEAR_FULL, CephReason.OSDS)

/** Postgres reasons worth waking someone for; a switchover or a missing replica is usually planned. */
private val CNPG_ALERT_REASONS = setOf(CnpgReason.ARCHIVING, CnpgReason.BACKUP_FAILED, CnpgReason.BACKUP_STALE)

/** MariaDB reasons worth waking someone for; a replica rolling out or a busy operator is usually planned. */
private val MARIADB_ALERT_REASONS = setOf(MariaDbReason.GALERA_RECOVERY, MariaDbReason.BACKUP_FAILED, MariaDbReason.BACKUP_STALE)
/** Percona reasons worth waking someone for; the operator still initializing is not one. */
private val PERCONA_ALERT_REASONS = setOf(PerconaReason.MEMBERS, PerconaReason.BACKUP_FAILED, PerconaReason.BACKUP_STALE)
/** Certificate reasons worth a warning; an issuer not ready alerts on its own. */
private val CERT_ALERT_REASONS = setOf(CertReason.EXPIRING, CertReason.RENEWAL_OVERDUE, CertReason.NOT_READY)
private val VELERO_ALERT_REASONS = setOf(VeleroReason.STALE, VeleroReason.PARTIALLY_FAILED, VeleroReason.INVALID)

/**
 * The problems of [services] worth a notification, keyed "system|label" (e.g. "longhorn|db/data")
 * with their severity: a faulted volume, an unavailable Garage cluster or a Postgres cluster without
 * any instance are critical; a degraded volume, a degraded Garage cluster (or blocks failing to
 * resync) and failing Postgres backups or archiving are warnings. An expired certificate (or one not
 * ready a week before it expires) is critical; one expiring, overdue or not ready, or an issuer not
 * ready, is a warning. A Velero schedule whose last backup failed or whose storage location is
 * unavailable is critical, a stale, partially failed or invalid one a warning. A Ceph cluster alerts
 * when critical, or on HEALTH_WARN, near-full capacity or an OSD down; a pool only when failed.
 */
fun dataIssuesOf(services: DataServices): Map<String, String> {
    val out = sortedMapOf<String, String>()
    out.flag("longhorn", services.longhorn?.volumes, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { it.serviceHealth == ServiceHealth.WARNING })
    out.flag("garage", services.garage?.instances, { it.label }, { it.state == GarageState.UNAVAILABLE }, { it.state == GarageState.DEGRADED || it.resyncErrors > 0 })
    out.flag("cnpg", services.cnpg?.clusters, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { c -> c.reasonList.any { it in CNPG_ALERT_REASONS } })
    // A rolling update is planned; a replica down or two masters are not.
    out.flag("dragonfly", services.dragonfly?.instances, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { d -> d.reasonList.any { it == DragonflyReason.PODS || it == DragonflyReason.MASTERS } })
    out.flag("mariadb", services.mariadb?.clusters, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { m -> m.reasonList.any { it in MARIADB_ALERT_REASONS } })
    out.flag("percona", services.percona?.clusters, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { c -> c.reasonList.any { it in PERCONA_ALERT_REASONS } })
    out.flag("certmanager", services.certManager?.certificates, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { c -> c.reasonList.any { it in CERT_ALERT_REASONS } })
    out.flag("certmanager", services.certManager?.issuers, { it.label }, { false }, { !it.ready })
    out.flag("velero", services.velero?.schedules, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { s -> s.reasonList.any { it in VELERO_ALERT_REASONS } })
    // A failed backup taken by hand was seen by whoever took it: shown, not alerted.
    out.flag("velero", services.velero?.locations, { "BackupStorageLocation/${it.label}" }, { it.serviceHealth == ServiceHealth.CRITICAL }, { false })
    out.flag("ceph", services.ceph?.clusters, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { c -> c.reasonList.any { it in CEPH_ALERT_REASONS } })
    // A pool alerts only when Rook reports it failed.
    out.flag("ceph", services.ceph?.pools, { "${it.kind}/${it.label}" }, { it.serviceHealth == ServiceHealth.CRITICAL }, { false })
    // A recommendation CAST AI cannot apply is critical; a read-only or HPA one was chosen: shown, not alerted.
    out.flag("castai", services.castai?.recommendations, { it.label }, { it.serviceHealth == ServiceHealth.CRITICAL }, { false })
    return out
}

/** Records each of [items] under "[system]|label": [critical] ones as critical, else [warning] ones as warnings. */
private inline fun <T> MutableMap<String, String>.flag(system: String, items: List<T>?, label: (T) -> String, critical: (T) -> Boolean, warning: (T) -> Boolean) {
    items.orEmpty().forEach { item ->
        when {
            critical(item) -> this["$system|${label(item)}"] = DATA_CRITICAL
            warning(item) -> this["$system|${label(item)}"] = DATA_WARNING
        }
    }
}
