package name.levis.ichor.model

import kotlinx.coroutines.CancellationException

/**
 * One page of a Kubernetes list from a Go page function (KubePodsPage...): [continueToken]
 * asks for the next one, [remaining] is how many items the next pages hold (-1 when the API
 * server does not say). [detailed]: full objects, not Table rows (pods: images, containers).
 */
data class KubePage<T>(
    val items: List<T>,
    val continueToken: String = "",
    val remaining: Long = -1,
    val complete: Boolean = true,
    val detailed: Boolean = true,
)

/** Prefix of the Go core's message for a 410 Gone (kube_client.go kubeListExpired). */
const val KUBE_LIST_EXPIRED = "Kubernetes API: list expired"

/** The API server expired the list being paged (its continue token is ~5 minutes old): load it again. */
fun Throwable.isKubeListExpired(): Boolean =
    generateSequence(this) { it.cause }.any { it.message?.contains(KUBE_LIST_EXPIRED) == true }

/** Rows a page function is asked for. */
const val KUBE_PAGE_SIZE = 500

/** Rows the eager load reads before it stops (L8): fewer on a metered network. */
fun eagerCap(metered: Boolean): Int = if (metered) 5_000 else 10_000

/**
 * A Kubernetes list loaded page by page:
 * [items] in the API server's order (namespace, then name), all of them once [done]. A load
 * that reached its cap stops [capped]; further pages then come on scroll ([loadMore]).
 */
data class PagedLoad<T>(
    val items: List<T> = emptyList(),
    /** Asks for the next page; "" before the first one and once [done]. */
    val continueToken: String = "",
    /** Items the server says remain after [items], -1 when unknown. */
    val remaining: Long = -1,
    val pages: Int = 0,
    val done: Boolean = false,
    val capped: Boolean = false,
    /** Every page held full objects: what only they carry (images) can be searched. */
    val detailed: Boolean = true,
) {
    /** More pages to load: the load stopped at its cap. */
    val hasMore: Boolean get() = pages > 0 && !done

    /** Loaded plus remaining rows; null while the server does not say. */
    val estimatedTotal: Long?
        get() = when {
            done -> items.size.toLong()
            remaining >= 0 -> items.size + remaining
            else -> null
        }

    fun append(page: KubePage<T>): PagedLoad<T> = copy(
        items = items + page.items,
        continueToken = if (page.complete) "" else page.continueToken,
        remaining = if (page.complete) 0 else page.remaining,
        pages = pages + 1,
        done = page.complete,
        detailed = detailed && page.detailed,
    )

    /** The next page, for a load that stopped at its cap; [this] once done. */
    suspend fun loadMore(fetch: suspend (token: String) -> KubePage<T>): PagedLoad<T> =
        if (hasMore) append(fetch(continueToken)) else this

    companion object {
        /** A list read whole (the last known one, kept only when complete); see [detailed]. */
        fun <T> complete(items: List<T>, detailed: Boolean = true): PagedLoad<T> =
            PagedLoad(items, remaining = 0, pages = 1, done = true, detailed = detailed)
    }
}

/**
 * Loads every page through [fetch] ("" for the first page), reporting each step to
 * [onProgress], until the list is done or holds [cap] rows (then marked capped). When the API
 * server expires the list mid-way it starts again from the first page, silently, up to
 * [maxRestarts] times (L12). Other errors propagate.
 */
suspend fun <T> loadPages(
    cap: Int,
    fetch: suspend (token: String) -> KubePage<T>,
    maxRestarts: Int = 3,
    onProgress: (PagedLoad<T>) -> Unit = {},
): PagedLoad<T> {
    var restarts = 0
    while (true) {
        try {
            return loadOnce(cap, fetch, onProgress)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            if (!e.isKubeListExpired() || restarts >= maxRestarts) throw e
            restarts++
        }
    }
}

private suspend fun <T> loadOnce(cap: Int, fetch: suspend (String) -> KubePage<T>, onProgress: (PagedLoad<T>) -> Unit): PagedLoad<T> {
    var load = PagedLoad<T>()
    do {
        load = load.append(fetch(load.continueToken))
        if (!load.done && load.items.size >= cap) load = load.copy(capped = true)
        onProgress(load)
    } while (!load.done && !load.capped)
    return load
}
