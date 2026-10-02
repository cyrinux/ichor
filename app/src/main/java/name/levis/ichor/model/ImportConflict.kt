package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * An imported context named like a stored one. [index] is its position in the imported
 * config's summary, [suggested] the free name it gets unless the user picks another, and
 * [sameAs] the stored context of the same cluster (same CA) it may replace instead.
 */
@Serializable
data class ImportConflict(
    val index: Int,
    val suggested: String,
    val sameAs: String? = null,
)

/** What the user picked for a conflict: a name of their own (blank: the suggested one), or replacing [ImportConflict.sameAs]. */
@Serializable
data class ImportChoice(
    val index: Int,
    val name: String = "",
    val replace: Boolean = false,
)

/**
 * The indexes of [choices] whose chosen name is already used: by a stored cluster, by a
 * context of the imported config ([imported]), by another choice, or as the suggested name
 * of a conflict left to it. The Go side refuses them too; this catches them while typing.
 */
fun takenNameChoices(
    conflicts: List<ImportConflict>,
    choices: List<ImportChoice>,
    stored: Set<String>,
    imported: List<String>,
): Set<Int> {
    val byIndex = choices.associateBy { it.index }
    val defaults = conflicts.filter { byIndex[it.index]?.let { c -> c.replace || c.name.isNotBlank() } != true }
        .map { it.suggested }
    val chosen = choices.filter { !it.replace && it.name.isNotBlank() }
    val counts = chosen.groupingBy { it.name.trim() }.eachCount()
    return chosen.filter { choice ->
        val name = choice.name.trim()
        name in stored || name in imported || name in defaults || (counts[name] ?: 0) > 1
    }.map { it.index }.toSet()
}
