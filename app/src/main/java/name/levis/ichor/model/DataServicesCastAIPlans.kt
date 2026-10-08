package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_castai_plans.go; the wire format is documented in
// plans/data-services/README.md.

/** One node consolidation CAST AI ran (a RebalancePlan): the nodes it removes and adds, and what it costs. */
@Serializable
data class CastAIPlan(
    val name: String = "",
    val createdAt: Long = 0,
    /** When it finished or failed, 0 while it runs. */
    val endedAt: Long = 0,
    /** full, delete-empty, drain-only. */
    val mode: String = "",
    /** As CAST AI writes it: Pending, Created, Running, Done, Failed, Skipped, Canceled, Expired. */
    val state: String = "",
    /** Executed without approval; false waits for one. */
    val execute: Boolean = false,
    val currency: String = "",
    /** Monthly cost of the nodes it touches, before and after. */
    val beforeMonthly: Double = 0.0,
    val afterMonthly: Double = 0.0,
    val savingsPercent: Double = 0.0,
    /** What it actually saved per month, when CAST AI measured it. */
    val achievedMonthly: Double? = null,
    val clusterMonthly: Double = 0.0,
    val clusterNodes: Int = 0,
    val failureReason: String = "",
    /** Creation or Deletion. */
    val failurePhase: String = "",
    /** The failure or skip message, in CAST AI's words. */
    val message: String = "",
    val warnings: List<String> = emptyList(),
    val removing: List<CastAIPlanNode> = emptyList(),
    val adding: List<CastAIPlanNode> = emptyList(),
    val budgets: List<CastAINodeBudget> = emptyList(),
) {
    val planState: CastAIPlanState get() = CastAIPlanState.from(state, execute)
    val planMode: CastAIPlanMode get() = CastAIPlanMode.from(mode)

    /** The monthly saving it aims for. */
    val plannedMonthly: Double get() = (beforeMonthly - afterMonthly).coerceAtLeast(0.0)

    /** The monthly saving it made: measured when CAST AI did, else planned; null unless it finished. */
    val savedMonthly: Double? get() = if (planState == CastAIPlanState.DONE) achievedMonthly ?: plannedMonthly else null

    /** "2 of 3": the nodes done out of those listed. */
    val removed: Int get() = removing.count { it.nodeStatus == CastAINodeStatus.SUCCESS }
    val added: Int get() = adding.count { it.nodeStatus == CastAINodeStatus.SUCCESS }
}

@Serializable
data class CastAIPlanNode(
    val name: String = "",
    /** Wire value of [CastAINodeStatus]. */
    val status: String = "",
    val instanceType: String = "",
    val spot: Boolean = false,
    val zone: String = "",
    val priceHourly: Double = 0.0,
    val events: List<CastAIPlanEvent> = emptyList(),
) {
    val nodeStatus: CastAINodeStatus get() = CastAINodeStatus.from(status)
}

/** One step of a node's creation or deletion: CAST AI's status (NodeCordoned, Blocked, Success...) and words. */
@Serializable
data class CastAIPlanEvent(val at: Long = 0, val status: String = "", val description: String = "")

/** A NodePool's disruption budget: how many of its nodes may be disrupted at once. */
@Serializable
data class CastAINodeBudget(val nodePool: String = "", val allowed: Int = 0, val disrupting: Int = 0, val nodes: Int = 0)

/** A node a plan failed to remove that a later plan tried again; [retrying] while one runs. */
@Serializable
data class CastAIStuckNode(val node: String = "", val failures: Int = 0, val retrying: Boolean = false)

enum class CastAIPlanState {
    AWAITING_APPROVAL, PENDING, RUNNING, DONE, FAILED, SKIPPED;

    companion object {
        fun from(state: String, execute: Boolean): CastAIPlanState = when (state) {
            "Running" -> RUNNING
            "Done" -> DONE
            "Failed" -> FAILED
            "Skipped", "Canceled", "Expired" -> SKIPPED
            else -> if (execute) PENDING else AWAITING_APPROVAL
        }
    }
}

enum class CastAIPlanMode(val wire: String) {
    FULL("full"),
    DELETE_EMPTY("delete-empty"),
    DRAIN_ONLY("drain-only"),
    OTHER(""),
    ;

    companion object {
        fun from(wire: String): CastAIPlanMode = entries.firstOrNull { it.wire == wire && wire.isNotEmpty() } ?: OTHER
    }
}

enum class CastAINodeStatus(val wire: String) {
    PENDING("pending"),
    IN_PROGRESS("inProgress"),
    BLOCKED("blocked"),
    SUCCESS("success"),
    FAILED("failed"),
    ;

    companion object {
        fun from(wire: String): CastAINodeStatus = entries.firstOrNull { it.wire == wire } ?: PENDING
    }
}

/** The plans of the last [windowMillis]: what finished ones saved, what failed ones did not, by state. */
data class CastAIPlanSummary(
    val currency: String,
    val savedMonthly: Double,
    val missedMonthly: Double,
    val done: Int,
    val failed: Int,
    val running: Int,
    val other: Int,
    val clusterMonthly: Double,
    val clusterNodes: Int,
)

const val CASTAI_SUMMARY_WINDOW_MILLIS = 24 * 60 * 60 * 1000L

fun CastAIStatus.planSummary(now: Long, windowMillis: Long = CASTAI_SUMMARY_WINDOW_MILLIS): CastAIPlanSummary {
    val recent = plans.filter { it.createdAt >= now - windowMillis }
    val states = recent.groupingBy { it.planState }.eachCount()
    val newest = plans.firstOrNull()
    return CastAIPlanSummary(
        currency = plans.firstOrNull { it.currency.isNotEmpty() }?.currency ?: "",
        savedMonthly = recent.sumOf { it.savedMonthly ?: 0.0 },
        missedMonthly = recent.filter { it.planState == CastAIPlanState.FAILED }.sumOf { it.plannedMonthly },
        done = states[CastAIPlanState.DONE] ?: 0,
        failed = states[CastAIPlanState.FAILED] ?: 0,
        running = (states[CastAIPlanState.RUNNING] ?: 0) + (states[CastAIPlanState.PENDING] ?: 0),
        other = (states[CastAIPlanState.SKIPPED] ?: 0) + (states[CastAIPlanState.AWAITING_APPROVAL] ?: 0),
        clusterMonthly = newest?.clusterMonthly ?: 0.0,
        clusterNodes = newest?.clusterNodes ?: 0,
    )
}

/** Plans grouped for the list: running and waiting first, then failed, then finished, each newest first. */
fun CastAIStatus.planGroups(): List<Pair<CastAIPlanState, List<CastAIPlan>>> {
    val order = listOf(
        CastAIPlanState.AWAITING_APPROVAL, CastAIPlanState.RUNNING, CastAIPlanState.PENDING,
        CastAIPlanState.FAILED, CastAIPlanState.DONE, CastAIPlanState.SKIPPED,
    )
    val byState = plans.groupBy { it.planState }
    return order.mapNotNull { state -> byState[state]?.let { state to it.sortedByDescending { p -> p.createdAt } } }
}
