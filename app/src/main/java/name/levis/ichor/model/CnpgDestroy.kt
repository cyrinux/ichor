package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** What removing one instance of a CloudNativePG cluster would do, like `kubectl cnpg destroy`. */
@Serializable
data class CnpgDestroyPreview(
    val namespace: String = "", val name: String = "", val instance: String = "",
    val currentPrimary: String = "", val targetPrimary: String = "",
    /** "" when the instance has no pod. */
    val podPhase: String = "", val podNode: String = "",
    val volumes: List<CnpgDestroyVolume> = emptyList(),
    /** primary, noOtherData or notFound: the removal is refused. */
    val blockers: List<String> = emptyList(),
    /** ready or nodePresent. */
    val warnings: List<String> = emptyList(),
    val canDestroy: Boolean = false,
)

@Serializable
data class CnpgDestroyVolume(
    val name: String = "",
    /** PG_DATA, PG_WAL, PG_TABLESPACE, "" on older clusters. */
    val role: String = "",
    val node: String = "",
    /** present, gone, unknown or none. */
    val nodeState: String = "",
)

/**
 * Instances worth removing: failed, without a pod, or waiting unscheduled; never the current or
 * target primary. The Go core checks again before deleting anything.
 */
val CnpgCluster.removableInstances: List<String>
    get() = (failedInstances + missingInstances + instancePods.filter { it.node.isEmpty() && it.phase == "Pending" }.map { it.name })
        .filter { it != currentPrimary && it != targetPrimary && it.isNotEmpty() }
        .distinct().sorted()
