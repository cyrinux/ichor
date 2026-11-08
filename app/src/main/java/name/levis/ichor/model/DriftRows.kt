package name.levis.ichor.model

/** How one itemised drift row reads: added on the node, missing from it, or changed. */
enum class DriftRowKind { ADDED, REMOVED, CHANGED }

/** One itemised drift row: [text] as shown (long values shortened), [full] to copy. */
data class DriftRow(val kind: DriftRowKind, val text: String, val full: String)

private const val MAX_VALUE_LENGTH = 16
private const val SHORT_VALUE_LENGTH = 8

/** [value] as is up to 16 characters, else its first 8 and "…" (a schematic hash, a long version). */
fun shortDriftValue(value: String): String =
    if (value.length > MAX_VALUE_LENGTH) value.take(SHORT_VALUE_LENGTH) + "…" else value

/** A `name=value` item with its value shortened; an item without `=` (a server, an address) stays whole. */
private fun shortDriftItem(item: String): String {
    val at = item.indexOf('=')
    return if (at < 0) item else item.substring(0, at + 1) + shortDriftValue(item.substring(at + 1))
}

private fun String.orEmptySign() = ifEmpty { "∅" }

/** The itemised rows, added then removed then changed; empty for a scalar key or an older core. */
fun DriftChange.rows(): List<DriftRow> =
    added.map { DriftRow(DriftRowKind.ADDED, shortDriftItem(it), it) } +
        removed.map { DriftRow(DriftRowKind.REMOVED, shortDriftItem(it), it) } +
        changed.map {
            DriftRow(
                DriftRowKind.CHANGED,
                "${it.name} ${shortDriftValue(it.before).orEmptySign()} → ${shortDriftValue(it.after).orEmptySign()}",
                "${it.name} ${it.before.orEmptySign()} → ${it.after.orEmptySign()}",
            )
        }

/** The whole setting as one "before → after" line, for scalar keys and older cores. */
fun DriftChange.beforeAfter(): String = "${before.orEmptySign()} → ${after.orEmptySign()}"
