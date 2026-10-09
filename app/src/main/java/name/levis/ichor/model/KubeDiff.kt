package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_diff.go and kube_flux_diff.go: what a GitOps tool would change in
// the cluster, object by object (the Linear plan document "D6. Flux", phase 5).

/** What reconciling a Flux object now would change. */
@Serializable
data class FluxDiff(
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    /** The source revision built. */
    val revision: String = "",
    /** The revision the controller last applied. */
    val applied: String = "",
    /** Sorted by the Go core: what needs a look first. */
    val resources: List<KubeDiffResource> = emptyList(),
    val warnings: List<String> = emptyList(),
) {
    /** Counts by change, in [DiffChange] order, the empty ones left out. */
    val counts: List<Pair<DiffChange, Int>>
        get() = resources.groupingBy { it.change }.eachCount().toSortedMap().map { it.key to it.value }

    /** The objects worth a look, then the unchanged ones (shown folded). */
    val changed: List<KubeDiffResource> get() = resources.filter { it.change != DiffChange.UNCHANGED }
    val unchanged: List<KubeDiffResource> get() = resources.filter { it.change == DiffChange.UNCHANGED }

    /** Nothing would change: every object is unchanged, ignored or not compared (encrypted). */
    val inSync: Boolean get() = resources.none { it.change.isChange || it.change == DiffChange.ERROR }

    /** A new source revision is built, not the one last applied. */
    val newRevision: Boolean get() = revision.isNotEmpty() && applied.isNotEmpty() && revision != applied
}

/** One object of a diff. */
@Serializable
data class KubeDiffResource(
    val group: String = "",
    val version: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val change: DiffChange = DiffChange.UNCHANGED,
    /** A unified diff from live to wanted, "" when unchanged or not compared. */
    val diff: String = "",
    /** The diff was cut at 64 KB. */
    val truncated: Boolean = false,
    /** Why the API server refused the dry run, for [DiffChange.ERROR]. */
    val error: String = "",
) {
    val key: String get() = "$group/$kind/$namespace/$name"

    /** The diff's lines without its "---"/"+++" header. */
    val lines: List<DiffLine> get() = parseDiffLines(diff)
}

/** How an object would change, in the order the screen lists them. */
@Serializable
enum class DiffChange {
    @SerialName("error") ERROR,
    @SerialName("created") CREATED,
    @SerialName("changed") CHANGED,
    @SerialName("deleted") DELETED,
    @SerialName("encrypted") ENCRYPTED,
    @SerialName("ignored") IGNORED,
    @SerialName("unchanged") UNCHANGED;

    /** A reconcile would write or delete something. */
    val isChange: Boolean get() = this == CREATED || this == CHANGED || this == DELETED
}

/** One line of a unified diff. */
data class DiffLine(val kind: Kind, val text: String) {
    enum class Kind { HUNK, CONTEXT, ADDED, REMOVED }
}

/** Reads a unified diff ("--- a", "+++ b" header, then "@@ … @@" hunks of " ", "-", "+" lines). */
fun parseDiffLines(diff: String): List<DiffLine> {
    if (diff.isEmpty()) return emptyList()
    val lines = diff.removeSuffix("\n").split('\n')
    // Only the header: a removed line can start with "--- " too.
    val header = if (lines.size >= 2 && lines[0].startsWith("--- ") && lines[1].startsWith("+++ ")) 2 else 0
    return lines.drop(header)
        .map { line ->
            when {
                line.startsWith("@@") -> DiffLine(DiffLine.Kind.HUNK, line)
                line.startsWith("+") -> DiffLine(DiffLine.Kind.ADDED, line.substring(1))
                line.startsWith("-") -> DiffLine(DiffLine.Kind.REMOVED, line.substring(1))
                else -> DiffLine(DiffLine.Kind.CONTEXT, line.removePrefix(" "))
            }
        }
}
