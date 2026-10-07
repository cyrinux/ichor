package name.levis.ichor.model

/** Lines replayed when following starts, like `talosctl logs -f --tail 200`. */
const val FOLLOW_TAIL_LINES = 200

/** Most lines kept while following; the oldest are dropped. */
const val MAX_FOLLOW_LINES = 5000

/** [this] plus [added], keeping only the last [cap]. */
fun <T> List<T>.appendCapped(added: List<T>, cap: Int = MAX_FOLLOW_LINES): List<T> =
    if (size + added.size <= cap) this + added else (this + added).takeLast(cap)

/** A followed log line, numbered so rows keep their keys as the oldest are dropped. */
data class FollowedLine(val seq: Long, val text: String)

/** The lines of a followed log, the newest [MAX_FOLLOW_LINES] kept. */
data class FollowBuffer(val lines: List<FollowedLine> = emptyList(), val nextSeq: Long = 0) {
    fun append(added: List<String>, cap: Int = MAX_FOLLOW_LINES): FollowBuffer {
        if (added.isEmpty()) return this
        val numbered = added.mapIndexed { i, text -> FollowedLine(nextSeq + i, text) }
        return FollowBuffer(lines.appendCapped(numbered, cap), nextSeq + added.size)
    }

    val text: String get() = lines.joinToString("\n") { it.text }
}
