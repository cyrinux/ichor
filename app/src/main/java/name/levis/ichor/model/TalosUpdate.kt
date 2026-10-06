package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo TalosUpdateCheck.

/** The latest Talos release compared with the versions the nodes run. */
@Serializable
data class TalosUpdateCheck(
    val latest: String = "",
    val latestDate: String = "",
    /** Some node runs an older version than [latest]. */
    val newer: Boolean = false,
    /** How many nodes run an older version. */
    val outdated: Int = 0,
    val oldest: String = "",
    /** Release notes URL. */
    val notes: String = "",
)

/** Minimum time between two checks (an internet call to GitHub). */
const val TALOS_UPDATE_INTERVAL_MILLIS = 6 * 60 * 60 * 1000L

/** Distinct versions of the reachable nodes, sorted, as passed to the check ("v1.14.0,v1.14.1"). */
fun nodeVersionsCsv(nodes: List<NodeOverview>): String =
    nodes.filter { it.reachable && it.version.isNotBlank() }.map { it.version }.distinct().sorted().joinToString(",")

/** Numeric parts of "v1.14.2-beta.1" -> [1, 14, 2]; empty when not a version. */
internal fun versionParts(version: String): List<Int> =
    Regex("""^v?(\d+)\.(\d+)\.(\d+)""").find(version.trim())?.groupValues?.drop(1)?.map { it.toInt() } ?: emptyList()

/**
 * Whether [version] is older than [latest] (by major.minor.patch; a pre-release of the same
 * numbers counts as older). False when either is not a version.
 */
fun isOlderVersion(version: String, latest: String): Boolean {
    val a = versionParts(version)
    val b = versionParts(latest)
    if (a.isEmpty() || b.isEmpty()) return false
    for (i in 0..2) if (a[i] != b[i]) return a[i] < b[i]
    return '-' in version && '-' !in latest
}

/** Reachable nodes running an older version than [latest], oldest first, for the upgrade chooser. */
fun outdatedNodes(nodes: List<NodeOverview>, latest: String): List<NodeOverview> =
    nodes.filter { it.reachable && isOlderVersion(it.version, latest) }
        .sortedWith { x, y ->
            when {
                isOlderVersion(x.version, y.version) -> -1
                isOlderVersion(y.version, x.version) -> 1
                else -> x.hostname.compareTo(y.hostname)
            }
        }

/** A newer release for the overview to offer, and how many reachable nodes are behind it. */
data class TalosUpdateOffer(val latest: String, val notes: String, val outdated: Int)

/**
 * What the overview offers from [check]: nothing when no reachable node is behind its release
 * (counted here: the check may have been asked for other versions), or when that very release
 * was [skipped] ("v1.14.2") for the cluster. The next release is offered again.
 */
fun talosUpdateOffer(check: TalosUpdateCheck, nodes: List<NodeOverview>, skipped: String?): TalosUpdateOffer? {
    if (check.latest.isEmpty() || check.latest == skipped) return null
    val outdated = outdatedNodes(nodes, check.latest).size
    return if (outdated == 0) null else TalosUpdateOffer(check.latest, check.notes, outdated)
}

/** Whether a check made at [lastAt] for [lastKey] can be reused for [key] at [now]. */
fun talosUpdateFresh(lastAt: Long, lastKey: String?, key: String, now: Long): Boolean =
    lastKey == key && lastAt > 0 && now - lastAt < TALOS_UPDATE_INTERVAL_MILLIS
