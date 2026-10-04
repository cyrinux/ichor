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
    services.dragonfly?.instances.orEmpty().forEach { d ->
        when {
            d.serviceHealth == ServiceHealth.CRITICAL -> out["dragonfly|${d.label}"] = DATA_CRITICAL
            // A rolling update is planned; a replica down or two masters are not.
            d.reasonList.any { it == DragonflyReason.PODS || it == DragonflyReason.MASTERS } -> out["dragonfly|${d.label}"] = DATA_WARNING
        }
    }
    services.mariadb?.clusters.orEmpty().forEach { m ->
        when {
            m.serviceHealth == ServiceHealth.CRITICAL -> out["mariadb|${m.label}"] = DATA_CRITICAL
            m.reasonList.any { it in MARIADB_ALERT_REASONS } -> out["mariadb|${m.label}"] = DATA_WARNING
        }
    }
    services.percona?.clusters.orEmpty().forEach { c ->
        when {
            c.serviceHealth == ServiceHealth.CRITICAL -> out["percona|${c.label}"] = DATA_CRITICAL
            c.reasonList.any { it in PERCONA_ALERT_REASONS } -> out["percona|${c.label}"] = DATA_WARNING
        }
    }
    services.certManager?.certificates.orEmpty().forEach { c ->
        when {
            c.serviceHealth == ServiceHealth.CRITICAL -> out["certmanager|${c.label}"] = DATA_CRITICAL
            c.reasonList.any { it in CERT_ALERT_REASONS } -> out["certmanager|${c.label}"] = DATA_WARNING
        }
    }
    services.certManager?.issuers.orEmpty().filter { !it.ready }.forEach { out["certmanager|${it.label}"] = DATA_WARNING }
    services.velero?.schedules.orEmpty().forEach { s ->
        when {
            s.serviceHealth == ServiceHealth.CRITICAL -> out["velero|${s.label}"] = DATA_CRITICAL
            s.reasonList.any { it in VELERO_ALERT_REASONS } -> out["velero|${s.label}"] = DATA_WARNING
        }
    }
    // A failed backup taken by hand was seen by whoever took it: shown, not alerted.
    services.velero?.locations.orEmpty().filter { it.serviceHealth == ServiceHealth.CRITICAL }.forEach { l ->
        out["velero|BackupStorageLocation/${l.label}"] = DATA_CRITICAL
    }
    services.ceph?.clusters.orEmpty().forEach { c ->
        when {
            c.serviceHealth == ServiceHealth.CRITICAL -> out["ceph|${c.label}"] = DATA_CRITICAL
            c.reasonList.any { it in CEPH_ALERT_REASONS } -> out["ceph|${c.label}"] = DATA_WARNING
        }
    }
    // A pool alerts only when Rook reports it failed.
    services.ceph?.pools.orEmpty().filter { it.serviceHealth == ServiceHealth.CRITICAL }.forEach { out["ceph|${it.kind}/${it.label}"] = DATA_CRITICAL }
    return out
}
