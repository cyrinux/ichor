package name.levis.ichor.model

/** How the workload list is cut: what needs a look first, only what grows, or per namespace. */
enum class CastAIView { OVERVIEW, GROWS, NAMESPACES }

/** What a section of the workload list holds; a namespace section names it in [CastAISection.namespace]. */
enum class CastAISectionKind { ATTENTION, GROWS, REDUCTIONS, OTHER, NAMESPACE }

/**
 * A titled run of recommendations. [hidden] are left out (the overview shows the first few that
 * grow); the deltas are the section's sums, shown for a namespace.
 */
data class CastAISection(
    val kind: CastAISectionKind,
    val rows: List<CastAIRecommendation>,
    val hidden: Int = 0,
    val namespace: String = "",
    val cpuDeltaMilli: Long = 0,
    val memoryDeltaBytes: Long = 0,
)

/** How many recommendations go each way. */
data class CastAIChangeCounts(val shrink: Int, val grow: Int, val same: Int, val unknown: Int)

fun CastAIStatus.changeCounts(): CastAIChangeCounts {
    val byChange = recommendations.groupingBy { it.change }.eachCount()
    return CastAIChangeCounts(
        shrink = byChange[CastAIChange.SHRINK] ?: 0,
        grow = byChange[CastAIChange.GROW] ?: 0,
        same = byChange[CastAIChange.SAME] ?: 0,
        unknown = byChange[CastAIChange.UNKNOWN] ?: 0,
    )
}

/** The apply mode most recommendations use and how many use it; null when there are none. */
fun CastAIStatus.commonMode(): Pair<CastAIMode, Int>? =
    recommendations.groupingBy { it.applyMode }.eachCount().maxByOrNull { it.value }?.toPair()

/** The overview shows this many workloads that grow before "show all". */
const val CASTAI_GROWS_PREVIEW = 5

/**
 * The workload list for [view], narrowed by [query] (namespace or workload). Biggest changes come
 * first: a workload that grows risks OOM kills and unschedulable pods, a shrinking one is the saving.
 */
fun CastAIStatus.sections(view: CastAIView, query: String = ""): List<CastAISection> {
    val q = query.trim()
    val shown = if (q.isEmpty()) recommendations else recommendations.filter { it.label.contains(q, ignoreCase = true) }
    val biggest = compareByDescending<CastAIRecommendation> { it.weight }.thenBy { it.label }
    val grows = shown.filter { it.change == CastAIChange.GROW }.sortedWith(compareByDescending<CastAIRecommendation> { it.nearMemoryLimit }.then(biggest))

    return when (view) {
        CastAIView.GROWS -> listOf(CastAISection(CastAISectionKind.GROWS, grows))
        CastAIView.NAMESPACES -> shown.groupBy { it.namespace }.toSortedMap().map { (ns, rows) ->
            CastAISection(
                CastAISectionKind.NAMESPACE, rows.sortedWith(biggest), namespace = ns,
                cpuDeltaMilli = rows.sumOf { it.cpuDeltaMilli }, memoryDeltaBytes = rows.sumOf { it.memoryDeltaBytes },
            )
        }
        CastAIView.OVERVIEW -> listOf(
            CastAISection(CastAISectionKind.ATTENTION, shown.filter { it.serviceHealth.needsAttention }.sortedBy { it.label }),
            CastAISection(CastAISectionKind.GROWS, grows.take(CASTAI_GROWS_PREVIEW), hidden = (grows.size - CASTAI_GROWS_PREVIEW).coerceAtLeast(0)),
            CastAISection(CastAISectionKind.REDUCTIONS, shown.filter { it.change == CastAIChange.SHRINK }.sortedWith(biggest)),
            CastAISection(CastAISectionKind.OTHER, shown.filter { it.change == CastAIChange.SAME || it.change == CastAIChange.UNKNOWN }.sortedBy { it.label }),
        )
    }.filter { it.rows.isNotEmpty() }
}
