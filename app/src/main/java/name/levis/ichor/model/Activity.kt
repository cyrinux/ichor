package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/audit.go.

/** One change the app made to a cluster (or tried to), from the action audit log. */
@Serializable
data class ActivityEntry(
    /** Unix millis. */
    val at: Long = 0,
    /** The talosconfig or kubeconfig context. */
    val cluster: String = "",
    val node: String = "",
    val namespace: String = "",
    /** "Kind/name". */
    @SerialName("object") val obj: String = "",
    /** A stable key: reboot, scale, rollout-restart, argo-sync... */
    val action: String = "",
    /** A summary of what the action was given, secrets redacted. */
    val params: String = "",
    /** ok or failed. */
    val outcome: String = "ok",
    val error: String = "",
    /** Done on the demo cluster. */
    val demo: Boolean = false,
)

val ActivityEntry.failed: Boolean get() = outcome != "ok"

/** What the entry acted on: the node, the namespace and the object that are set, joined by "/". */
val ActivityEntry.target: String get() = listOf(node, namespace, obj).filter { it.isNotEmpty() }.joinToString("/")

/** "rollout-restart" as "Rollout restart": the keys are plain English words. */
val ActivityEntry.actionLabel: String
    get() = action.replace('-', ' ').replaceFirstChar { it.uppercase() }

/** The filters of the activity screen: null for "any". */
data class ActivityFilter(val cluster: String? = null, val action: String? = null, val failedOnly: Boolean = false)

fun ActivityEntry.matches(filter: ActivityFilter): Boolean =
    (filter.cluster == null || cluster == filter.cluster) &&
        (filter.action == null || action == filter.action) &&
        (!filter.failedOnly || failed)

/** The distinct values of [key] in [entries], in order of first appearance (newest first). */
fun List<ActivityEntry>.distinctOf(key: (ActivityEntry) -> String): List<String> = map(key).filter { it.isNotEmpty() }.distinct()
