package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

/**
 * An object opened lately (a node, workload, pod, CronJob, Argo CD or Flux app), for the
 * search's "Recently opened": the screen it opens as a share link names it, the [label] and
 * [detail] it was shown with, and when ([at], epoch ms). Same JSON as iOS's `RecentTarget`.
 */
@Serializable
data class RecentTarget(val target: ShareTarget, val label: String, val detail: String = "", val at: Long = 0) {
    /** The object it names, whatever tab it was opened on. */
    val key: ShareTarget get() = target.copy(tab = "")
}

/** The phone's recently opened objects, newest first: their stored form and how one is added. */
object RecentTargets {
    /** At most this many: a few to come back to during an incident, not a history. */
    const val MAX = 20

    /** What is recorded: objects a link can name, not the cluster-wide screens. */
    val RECORDED = Pins.PINNABLE

    private val json = Json { ignoreUnknownKeys = true }
    private val serializer = ListSerializer(RecentTarget.serializer())

    /** Reads [encode]'s form; anything unreadable is none, repeats and extras are dropped. */
    fun parse(text: String?): List<RecentTarget> {
        if (text.isNullOrBlank()) return emptyList()
        val recent = runCatching { json.decodeFromString(serializer, text) }.getOrNull().orEmpty()
        return recent.filter { it.target.target in RECORDED }.distinctBy { it.key }.take(MAX)
    }

    fun encode(recent: List<RecentTarget>): String = json.encodeToString(serializer, recent)

    /**
     * [recent] with [opened] first, its earlier entry dropped, at most [MAX]; unchanged for what
     * is not recorded or names no cluster (a link could not open it again).
     */
    fun adding(recent: List<RecentTarget>, opened: RecentTarget): List<RecentTarget> {
        if (opened.target.target !in RECORDED || opened.target.cluster.isEmpty() || opened.label.isBlank()) return recent
        return (listOf(opened) + recent.filterNot { it.key == opened.key }).take(MAX)
    }

    fun removing(recent: List<RecentTarget>, target: ShareTarget): List<RecentTarget> {
        val key = target.copy(tab = "")
        return recent.filterNot { it.key == key }
    }

    /** Those of a cluster still on the phone ([clusterIds]): a removed cluster's are not shown. */
    fun known(recent: List<RecentTarget>, clusterIds: Set<String>): List<RecentTarget> =
        recent.filter { it.target.cluster in clusterIds }
}

/** The share target the Kubernetes screen focused on names; null when it focuses no object. */
fun KubeFocus.shareTarget(): ShareTarget? {
    if (name.isEmpty()) return null
    return when (tab) {
        0 -> key.split('/').takeIf { it.size == 3 }?.let { (kind, ns, n) -> ShareTarget.workload(kind, ns, n) }
        1 -> ShareTarget.pod(namespace, name)
        2 -> ShareTarget.cronJob(namespace, name)
        else -> null
    }
}
