package name.levis.ichor.model

import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope

/** The workload kinds the Go core pages, one list each. */
val WORKLOAD_KINDS = listOf("Deployment", "StatefulSet", "DaemonSet")

/**
 * The kinds still to load and their continue tokens, from a token [workloadToken] made: ""
 * (the first page) starts every kind. Go's tokens are hex, so '=' and ';' never occur in them.
 */
fun parseWorkloadToken(token: String): Map<String, String> =
    if (token.isEmpty()) {
        WORKLOAD_KINDS.associateWith { "" }
    } else {
        token.split(';').mapNotNull { part ->
            part.split('=', limit = 2).takeIf { it.size == 2 && it[0] in WORKLOAD_KINDS }?.let { it[0] to it[1] }
        }.toMap()
    }

/** One token for the kinds still to load (each with its next page's token). */
fun workloadToken(pending: Map<String, String>): String =
    WORKLOAD_KINDS.filter { it in pending }.joinToString(";") { "$it=${pending.getValue(it)}" }

/**
 * The next page of every kind still to load, asked side by side through [fetch] and merged
 * into one page: complete once every kind is, its remaining count unknown while any kind's is.
 */
suspend fun fetchWorkloadPage(
    token: String,
    fetch: suspend (kind: String, token: String) -> KubePage<KubeWorkload>,
): KubePage<KubeWorkload> = coroutineScope {
    val pending = parseWorkloadToken(token)
    val pages = pending.map { (kind, kindToken) -> async { kind to fetch(kind, kindToken) } }.awaitAll()
    val next = pages.filter { (_, page) -> !page.complete }.associate { (kind, page) -> kind to page.continueToken }
    val remaining = pages.filter { !it.second.complete }.map { it.second.remaining }
    KubePage(
        items = pages.flatMap { it.second.items },
        continueToken = workloadToken(next),
        remaining = if (remaining.any { it < 0 }) -1 else remaining.sum(),
        complete = next.isEmpty(),
    )
}
