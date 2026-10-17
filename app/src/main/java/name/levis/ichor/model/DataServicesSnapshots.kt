package name.levis.ichor.model

import kotlinx.serialization.Serializable

@Serializable
data class VolumeSnapshots(
    val installed: Boolean = false, val version: String = "", val error: String = "",
    val contentsError: String = "", val classesError: String = "", val claimsError: String = "",
    val snapshots: List<VolumeSnapshot> = emptyList(), val contents: List<SnapshotContent> = emptyList(),
    val classes: List<SnapshotClass> = emptyList(), val claims: List<SnapshotClaim> = emptyList(),
) {
    fun summary(): ServiceSummary {
        val attention = claims.count { it.watched && it.stale } + snapshots.count { it.error.isNotEmpty() }
        return ServiceSummary(snapshots.size, attention, if (attention > 0) ServiceHealth.WARNING else ServiceHealth.OK,
            listOf(error, contentsError, classesError, claimsError).filter { it.isNotEmpty() }.joinToString("; "))
    }
    fun canCreate(claim: StorageClaim): Boolean = installed && classesError.isEmpty() && claim.phase == "Bound" &&
        !claim.terminating && claim.provisioner.isNotEmpty() && classes.count { it.default && it.driver == claim.provisioner } == 1
}
@Serializable
data class VolumeSnapshot(
    val namespace: String = "", val name: String = "", val pvc: String = "", @kotlinx.serialization.SerialName("class") val className: String = "",
    val content: String = "", val ready: Boolean = false, val createdAt: Long = 0, val ageSeconds: Long = -1, val error: String = "",
) {
    val key: String get() = "$namespace/$name"
}
@Serializable
data class SnapshotContent(
    val name: String = "", val namespace: String = "", val snapshot: String = "",
    @kotlinx.serialization.SerialName("class") val className: String = "",
    val driver: String = "", val ready: Boolean = false, val createdAt: Long = 0, val error: String = "",
)
@Serializable
data class SnapshotClass(val name: String = "", val driver: String = "", val deletionPolicy: String = "", val default: Boolean = false)
@Serializable
data class SnapshotClaim(
    val namespace: String = "", val name: String = "", val lastGoodAt: Long = 0, val ageSeconds: Long = -1,
    val watched: Boolean = false, val stale: Boolean = false,
)
@Serializable
data class BackupJobStatus(val error: String = "", val jobsError: String = "", val jobs: List<BackupCronJob> = emptyList()) {
    fun summary(): ServiceSummary = ServiceSummary(jobs.size, jobs.count { ServiceHealth.from(it.health).needsAttention },
        ServiceHealth.worst(jobs.map { ServiceHealth.from(it.health) }), listOf(error, jobsError).filter { it.isNotEmpty() }.joinToString("; "))
}
@Serializable
data class BackupCronJob(
    val namespace: String = "", val name: String = "", val schedule: String = "", val suspended: Boolean = false,
    val lastSuccess: Long = 0, val ageSeconds: Long = -1, val failureStreak: Int = 0, val health: String = "",
) {
    val label: String get() = "$namespace/$name"

    /** The row's dot: critical after a failed run, a warning when suspended or no success in a week. */
    fun rowHealth(now: Long): ServiceHealth = when {
        failureStreak > 0 -> ServiceHealth.CRITICAL
        suspended || lastSuccess <= 0 || now - lastSuccess > BACKUP_STALE_MS -> ServiceHealth.WARNING
        else -> ServiceHealth.OK
    }
}

/** 7 days without a successful backup run. */
const val BACKUP_STALE_MS = 7L * 24 * 60 * 60 * 1000
