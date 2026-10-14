package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_velero_restore.go.

/** What restoring one Velero backup holds, whether it can be, and the Restores of it already there. */
@Serializable
data class VeleroRestorePreview(
    val namespace: String = "",
    val backup: String = "",
    /** The Backup's own: Completed, PartiallyFailed, Failed, FailedValidation, InProgress… */
    val phase: String = "",
    val completedAt: Long = 0,
    /** Unix ms, 0 when it never expires. */
    val expiresAt: Long = 0,
    val storageLocation: String = "",
    val locationAvailable: Boolean = false,
    /** Empty or "*": every namespace. */
    val includedNamespaces: List<String> = emptyList(),
    val excludedNamespaces: List<String> = emptyList(),
    val includedResources: List<String> = emptyList(),
    /** The Backup's own: "true", "false" or "unset". */
    val includeClusterResources: String = "unset",
    val snapshotVolumes: Boolean? = null,
    val defaultVolumesToFsBackup: Boolean? = null,
    val itemsBackedUp: Int = 0,
    val totalItems: Int = 0,
    val volumeSnapshots: Int = 0,
    val errors: Int = 0,
    val warnings: Int = 0,
    val canRestore: Boolean = false,
    /** Why it cannot (or had better not) be restored: see [VeleroRestoreReason]. */
    val reasons: List<String> = emptyList(),
    /** The Restores of this backup, newest first; the first one running is the one being followed. */
    val restores: List<VeleroRestoreDetail> = emptyList(),
    /** The Restores or the storage locations could not be read: the backup still shows. */
    val error: String = "",
)

/** One Restore of the backup: the progress the apps follow after launching one. */
@Serializable
data class VeleroRestoreDetail(
    val name: String = "",
    /** New, InProgress, WaitingForPluginOperations…, Finalizing…, Completed, PartiallyFailed, Failed. */
    val phase: String = "",
    val startedAt: Long = 0,
    val completedAt: Long = 0,
    val errors: Int = 0,
    val warnings: Int = 0,
    val failureReason: String = "",
    /** What the restore controller refused, when it set FailedValidation. */
    val validationErrors: List<String> = emptyList(),
    val itemsRestored: Int = 0,
    val totalItems: Int = 0,
    val namespaceMapping: Map<String, String> = emptyMap(),
    val includedNamespaces: List<String> = emptyList(),
    val existingResourcePolicy: String = "",
) {
    val running: Boolean get() = phase == "New" || phase == "InProgress" || phase.startsWith("WaitingForPluginOperations") || phase.startsWith("Finalizing")
    val finished: Boolean get() = phase == "Completed" || phase == "PartiallyFailed" || phase == "Failed" || phase == "FailedValidation"
}

/** Why a backup cannot (or had better not) be restored, as the Go core names it. */
enum class VeleroRestoreReason(val wire: String) {
    PARTIALLY_FAILED("partiallyFailed"),
    LOCATION_UNAVAILABLE("locationUnavailable"),
    EXPIRED("expired"),
    RESTORE_RUNNING("restoreRunning"),
    ALL_NAMESPACES("allNamespaces"),
    ;

    companion object {
        fun from(wire: String): VeleroRestoreReason? = entries.firstOrNull { it.wire == wire }
    }
}

/** The options of a restore, serialized to JSON for the Go core (gomobile passes strings only). */
@Serializable
data class VeleroRestoreOptions(
    /** Empty: every namespace of the backup. */
    val includedNamespaces: List<String> = emptyList(),
    /** Source namespace → target namespace; a target that does not exist is created. */
    val namespaceMapping: Map<String, String> = emptyMap(),
    /** "" or "none": leave existing objects; "update": overwrite them. */
    val existingResourcePolicy: String = "none",
    /** Null: Velero's default (restore them). */
    val restorePVs: Boolean? = null,
    val includeClusterResources: Boolean? = null,
)
