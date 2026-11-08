package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_actions.go.

/** The most replicas the core accepts for a scale. */
const val MAX_SCALE_REPLICAS = 1000

/** Lines of a pod log read at once (the core caps them at 5000). */
const val POD_LOG_TAIL = 2000

/** A workload's rollout history (KubeWorkloadRevisions). */
@Serializable
data class KubeRevisionList(
    val kind: String = "",
    /** The pods only take a rolled back template when deleted (OnDelete, StatefulSet partition). */
    val manual: Boolean = false,
    /** A paused Deployment: its rollback is refused until the rollout resumes. */
    val paused: Boolean = false,
    val revisions: List<KubeRevision> = emptyList(),
)

/**
 * One kept revision of a workload, as `kubectl rollout history` lists it: one of a
 * Deployment's ReplicaSets, or one of a StatefulSet's or DaemonSet's ControllerRevisions.
 */
@Serializable
data class KubeRevision(
    val revision: Int,
    /** The ReplicaSet, or the ControllerRevision, that keeps it. */
    val replicaSet: String = "",
    /** Unix millis. */
    val created: Long = 0,
    val images: List<String> = emptyList(),
    /** The kubernetes.io/change-cause annotation; "" when not set. */
    val changeCause: String = "",
    val replicas: Int = 0,
    /** The revision the workload runs now: nothing to roll back to. */
    val current: Boolean = false,
)

/** The pod template diff from the current revision to an older one (KubeRevisionDiff). */
@Serializable
data class KubeRevisionDiff(
    /** Unified; "" when the templates are equal. */
    val diff: String = "",
    val from: String = "",
    val to: String = "",
    val fromRevision: Int = 0,
    val toRevision: Int = 0,
    val images: KubeRevisionImages = KubeRevisionImages(),
)

@Serializable
data class KubeRevisionImages(val from: List<String> = emptyList(), val to: List<String> = emptyList())

/** One image change of a rollback, [from] what runs now [to] what the revision ran; "" for none. */
data class ImageChange(val from: String, val to: String)

/**
 * The images a rollback changes, container by container (the lists are in container order):
 * only those that differ; a container only one side has comes with "" on the other.
 */
fun KubeRevisionImages.changes(): List<ImageChange> =
    (0 until maxOf(from.size, to.size))
        .map { ImageChange(from.getOrElse(it) { "" }, to.getOrElse(it) { "" }) }
        .filter { it.from != it.to }

/** Deployments and StatefulSets scale; a DaemonSet runs one pod per node. */
val KubeWorkload.canScale: Boolean get() = kind == "Deployment" || kind == "StatefulSet"

/** Deployments (ReplicaSets), StatefulSets and DaemonSets (ControllerRevisions) keep revisions to roll back to. */
val KubeWorkload.hasHistory: Boolean get() = kind == "Deployment" || kind == "StatefulSet" || kind == "DaemonSet"

/** Only a Deployment's rollout can be paused. */
val KubeWorkload.canPauseRollout: Boolean get() = kind == "Deployment"

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
