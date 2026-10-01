package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Progress of one node of a support bundle (`talosctl support`), as sent by the Go core. */
@Serializable
data class SupportProgress(val node: String = "", val step: String = "", val done: Int = 0, val total: Int = 0)

val SupportProgress.fraction: Float? get() = if (total > 0) (done.toFloat() / total).coerceIn(0f, 1f) else null

val SupportProgress.finished: Boolean get() = total > 0 && done >= total

private val STAMP = DateTimeFormatter.ofPattern("yyyyMMdd-HHmmss", Locale.ROOT)
private val UNSAFE = Regex("[^A-Za-z0-9._-]+")
private val BUNDLE_NAME = Regex("""support-[A-Za-z0-9._-]+\.zip""")

/** "support-<context>-<yyyyMMdd-HHmmss>.zip", safe as a file name whatever the context is called. */
fun supportBundleFileName(context: String, nowMillis: Long, zone: ZoneId = ZoneId.systemDefault()): String {
    val name = context.replace(UNSAFE, "-").trim('-', '.').take(40).ifEmpty { "cluster" }
    return "support-$name-${STAMP.format(Instant.ofEpochMilli(nowMillis).atZone(zone))}.zip"
}

/** Only files this app wrote are listed, shared or deleted. */
fun isSupportBundleName(name: String): Boolean = BUNDLE_NAME.matches(name)

/** Node name of the cluster-wide part of a bundle (etcd, then writing the archive). */
const val BUNDLE_CLUSTER = ""

/** Where one node (or the cluster-wide part, [BUNDLE_CLUSTER]) of a running bundle stands. */
enum class BundleRowState { WAITING, COLLECTING, DONE }

/**
 * Progress of a running bundle: the latest report of each node. The core counts steps per
 * node and closes each one with done == total.
 */
data class BundleProgress(val byNode: Map<String, SupportProgress> = emptyMap()) {
    fun with(progress: SupportProgress): BundleProgress = BundleProgress(byNode + (progress.node to progress))

    fun stateOf(node: String): BundleRowState {
        val latest = byNode[node] ?: return BundleRowState.WAITING
        return if (latest.finished) BundleRowState.DONE else BundleRowState.COLLECTING
    }

    /** The step [node] is at, or null when it is waiting or finished. */
    fun stepOf(node: String): String? = byNode[node]?.takeIf { !it.finished }?.step

    /** Finished rows over all rows: the selected [nodes] plus the cluster-wide part. */
    fun overallFraction(nodes: List<String>): Float {
        val rows = nodes + BUNDLE_CLUSTER
        return rows.count { stateOf(it) == BundleRowState.DONE }.toFloat() / rows.size
    }
}

/** Nodes preselected for a bundle: all of them (an unreachable one only reports its error). */
fun defaultBundleNodes(nodes: List<NodeOverview>): Set<String> = nodes.map { it.node }.toSet()
