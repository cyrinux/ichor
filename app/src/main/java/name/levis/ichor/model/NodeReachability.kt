package name.levis.ichor.model

/**
 * Whether a node answers its Talos API, as the overview (or a call that just failed) says:
 * [errorKind] network, tls, auth or other, [error] the core's words, [lastSeen] when it last
 * answered (epoch millis). Unknown is null, never an unreachable value: a node is only dimmed
 * on evidence.
 */
data class NodeReachability(
    val reachable: Boolean,
    val errorKind: String? = null,
    val error: String? = null,
    val lastSeen: Long? = null,
)

/** The core's prefix for a node it could not dial (friendlyError in go/ichorgo/errors.go). */
private const val UNREACHABLE_PREFIX = "unreachable: "

/** [node] as this overview saw it; null when the overview does not list it. */
fun ClusterOverview.reachabilityOf(node: String): NodeReachability? =
    nodes.find { it.node == node }?.let { NodeReachability(it.reachable, it.errorKind, it.error, it.lastSeen) }

/**
 * An unreachable node from the [message] of a call to it that failed because it could not be
 * dialled; null for any other failure (refused, too old, timed out on a slow answer).
 */
fun unreachableFrom(message: String?): NodeReachability? =
    message?.takeIf { it.startsWith(UNREACHABLE_PREFIX) }?.let { NodeReachability(reachable = false, errorKind = "network", error = it) }

/**
 * What the hub knows of a node: a [live] failure of a call to it wins over what the last
 * overview said ([known]), as the node may have gone down since; null while neither knows.
 * The [known] last-answered time is kept.
 */
fun mergeReachability(known: NodeReachability?, live: NodeReachability?): NodeReachability? = when {
    live == null -> known
    known == null || known.reachable -> live
    else -> known
}

/** True only when the node is known not to answer; unknown counts as reachable. */
val NodeReachability?.down: Boolean get() = this?.reachable == false
