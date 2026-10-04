package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_actions.go.

/** The most replicas the core accepts for a scale. */
const val MAX_SCALE_REPLICAS = 1000

/** Lines of a pod log read at once (the core caps them at 5000). */
const val POD_LOG_TAIL = 2000

@Serializable
data class KubeRevisionList(val revisions: List<KubeRevision> = emptyList())

/** One kept revision of a Deployment (one of its ReplicaSets), as `kubectl rollout history` lists it. */
@Serializable
data class KubeRevision(
    val revision: Int,
    val replicaSet: String = "",
    /** Unix millis. */
    val created: Long = 0,
    val images: List<String> = emptyList(),
    /** The kubernetes.io/change-cause annotation; "" when not set. */
    val changeCause: String = "",
    val replicas: Int = 0,
    /** The revision the Deployment runs now: nothing to roll back to. */
    val current: Boolean = false,
)

/** Deployments and StatefulSets scale; a DaemonSet runs one pod per node. */
val KubeWorkload.canScale: Boolean get() = kind == "Deployment" || kind == "StatefulSet"

/** Only Deployments keep revisions to roll back to. */
val KubeWorkload.hasHistory: Boolean get() = kind == "Deployment"

/** A replica count within what the core accepts. */
fun clampReplicas(replicas: Int): Int = replicas.coerceIn(0, MAX_SCALE_REPLICAS)

/** Removing replicas stops pods: a scale-down to more than 0 is confirmed (0 needs the typed name). */
fun scaleNeedsConfirm(current: Int, target: Int): Boolean = target in 1 until current

/** Scaling to 0 stops every pod: the user types the workload's name to confirm. */
fun scaleNeedsTypedName(replicas: Int): Boolean = replicas == 0

/**
 * The containers to choose from when the API refused a log read without a container ("a
 * container name must be specified for pod P, choose one of: [a b] or one of the init
 * containers: [i]"): the regular ones, then the init ones; empty for any other error.
 */
fun containersToChoose(error: String): List<String> {
    if (!error.contains("container name must be specified")) return emptyList()
    return Regex("""\[([^\]]*)]""").findAll(error).flatMap { m -> m.groupValues[1].split(' ').filter { it.isNotBlank() } }.distinct().toList()
}

/** A pod log split in lines, without the trailing empty line of the last newline. */
fun logLines(text: String): List<String> = if (text.isEmpty()) emptyList() else text.removeSuffix("\n").split('\n')
