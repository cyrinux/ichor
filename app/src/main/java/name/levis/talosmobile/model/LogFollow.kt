package name.levis.talosmobile.model

/** Lines replayed when following starts, like `talosctl logs -f --tail 200`. */
const val FOLLOW_TAIL_LINES = 200

/** Most lines kept while following; the oldest are dropped. */
const val MAX_FOLLOW_LINES = 5000

/** [lines] plus [added], keeping only the last [cap]. */
fun List<String>.appendCapped(added: List<String>, cap: Int = MAX_FOLLOW_LINES): List<String> =
    if (size + added.size <= cap) this + added else (this + added).takeLast(cap)

/** Lines containing [filter] (case-insensitive); all of them when it is blank. */
fun List<String>.matching(filter: String): List<String> =
    if (filter.isBlank()) this else filter { it.contains(filter, ignoreCase = true) }
