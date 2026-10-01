package name.levis.talosmobile.model

/** Lines replayed when following starts, like `talosctl logs -f --tail 200`. */
const val FOLLOW_TAIL_LINES = 200

/** Most lines kept while following; the oldest are dropped. */
const val MAX_FOLLOW_LINES = 5000

/** [this] plus [added], keeping only the last [cap]. */
fun <T> List<T>.appendCapped(added: List<T>, cap: Int = MAX_FOLLOW_LINES): List<T> =
    if (size + added.size <= cap) this + added else (this + added).takeLast(cap)
