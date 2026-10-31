package name.levis.ichor.model

/** Longest tag: it sits on a chip. */
const val CLUSTER_TAG_MAX = 24

/** Most tags on one cluster. */
const val CLUSTER_TAGS_MAX = 8

private val TAG_SEPARATORS = Regex("[,\\s]+")

/** A line typed for a cluster's tags, split on commas and spaces: lowercase, capped, no empties or repeats. */
fun normalizeTags(input: String): List<String> =
    input.split(TAG_SEPARATORS)
        .map { it.lowercase().take(CLUSTER_TAG_MAX) }
        .filter { it.isNotEmpty() }
        .distinct()
        .take(CLUSTER_TAGS_MAX)

/** The tags of [saved] (by fingerprint) whose cluster is still among [fingerprints]. */
fun keepClusterTags(saved: Map<String, List<String>>, fingerprints: List<String>): Map<String, List<String>> {
    val known = fingerprints.filter { it.isNotBlank() }.toSet()
    return saved.filterKeys { it in known }
}

/** The tags of a cluster whose contexts are [fingerprints]: a context added later has none of its own yet. */
fun clusterTagsOf(saved: Map<String, List<String>>, fingerprints: List<String>): List<String> =
    fingerprints.flatMap { saved[it].orEmpty() }.distinct()

/** The tags of the clusters other than [own] (its fingerprints), minus those already in [typed], sorted. */
fun tagSuggestions(saved: Map<String, List<String>>, own: List<String>, typed: String): List<String> {
    val present = normalizeTags(typed).toSet()
    return saved.filterKeys { it !in own }.values.flatten().distinct().filter { it !in present }.sorted()
}

/** What of [selected] still means something for [groups] ("" is the untagged clusters): a stale tag never hides the fleet. */
fun activeTagFilter(groups: List<FleetGroup>, selected: Set<String>): Set<String> =
    selected.intersect(groups.map { it.tag }.toSet())

/** The ranked [rows] in any group of [selected]; all of them when none of it applies. */
fun fleetRowsWithTags(rows: List<FleetRow>, groups: List<FleetGroup>, selected: Set<String>): List<FleetRow> {
    val active = activeTagFilter(groups, selected)
    if (active.isEmpty()) return rows
    val keys = groups.filter { it.tag in active }.flatMap { it.keys }.toSet()
    return rows.filter { it.key in keys }
}
