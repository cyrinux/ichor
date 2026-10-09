package name.levis.ichor.model

/**
 * One change of a list the Go core keeps live (StartKubeWatch, KubeWatchListener): the whole
 * list at the start and whenever the watch started over, else one item to merge into it.
 */
sealed interface KubeWatchEvent<out T> {
    data class Sync<T>(val items: List<T>) : KubeWatchEvent<T>
    data class Added<T>(val item: T) : KubeWatchEvent<T>
    data class Modified<T>(val item: T) : KubeWatchEvent<T>
    data class Deleted<T>(val item: T) : KubeWatchEvent<T>

    companion object {
        /**
         * What the listener got as [eventType] and [json]: an item decoded with [item], a SYNC
         * list with [list]; null for an unknown type or JSON the models cannot read.
         */
        fun <T> decode(eventType: String, json: String, item: (String) -> T, list: (String) -> List<T>): KubeWatchEvent<T>? = runCatching {
            when (eventType) {
                "SYNC" -> Sync(list(json))
                "ADDED" -> Added(item(json))
                "MODIFIED" -> Modified(item(json))
                "DELETED" -> Deleted(item(json))
                else -> null
            }
        }.getOrNull()
    }
}

/**
 * This list with [event] applied, rows told apart by [key]: a changed row keeps its place, a
 * new one goes last (the API server's order ends there), a deleted one leaves.
 */
fun <T> List<T>.applying(event: KubeWatchEvent<T>, key: (T) -> String): List<T> = when (event) {
    is KubeWatchEvent.Sync -> event.items
    is KubeWatchEvent.Added -> upserting(event.item, key)
    is KubeWatchEvent.Modified -> upserting(event.item, key)
    is KubeWatchEvent.Deleted -> key(event.item).let { gone -> filterNot { key(it) == gone } }
}

private fun <T> List<T>.upserting(item: T, key: (T) -> String): List<T> {
    val at = indexOfFirst { key(it) == key(item) }
    return if (at < 0) this + item else mapIndexed { i, row -> if (i == at) item else row }
}

/**
 * This load with [event] applied: a SYNC is the whole list, complete and as full objects
 * (nothing left to page); a single change keeps the pages as they are.
 */
fun <T> PagedLoad<T>.applying(event: KubeWatchEvent<T>, key: (T) -> String): PagedLoad<T> = when (event) {
    is KubeWatchEvent.Sync -> PagedLoad.complete(event.items)
    else -> copy(items = items.applying(event, key))
}
