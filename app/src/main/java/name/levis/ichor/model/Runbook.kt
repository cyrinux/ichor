package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** The most a runbook file may weigh (the Go core's own limit). */
const val RUNBOOK_MAX_BYTES = 64 * 1024

/** A runbook kept for a cluster (RunbookStoreList's row); [problem] when it no longer loads. */
@Serializable
data class RunbookItem(
    val id: String,
    val name: String,
    val description: String = "",
    val steps: Int = 0,
    val destructive: Int = 0,
    val importedAt: Long = 0,
    val problem: String = "",
)

@Serializable
data class RunbookList(val items: List<RunbookItem> = emptyList())

/** A value a runbook asks for before its plan (RunbookSummary's params). */
@Serializable
data class RunbookParam(val name: String, val prompt: String = name, val default: String = "", val kind: String = KIND_STRING) {
    companion object {
        const val KIND_STRING = "string"
        const val KIND_NAMESPACE = "namespace"
        const val KIND_WORKLOAD = "workload"
        const val KIND_NODE = "node"
        const val KIND_INT = "int"
        const val KIND_BOOL = "bool"
    }
}

/** RunbookSummary's answer, for what the plan asks first. */
@Serializable
data class RunbookSummary(val params: List<RunbookParam> = emptyList())

/** The names among [params] that have neither a value nor a default. */
fun missingRunbookValues(params: List<RunbookParam>, values: Map<String, String>): List<String> =
    params.filter { values[it.name].isNullOrBlank() && it.default.isBlank() }.map { it.name }

/** What the form starts from: the [remembered] values of a parameter that still exists, else its default. */
fun initialRunbookValues(params: List<RunbookParam>, remembered: Map<String, String>): Map<String, String> =
    params.associate { it.name to (remembered[it.name]?.takeIf(String::isNotBlank) ?: it.default) }

/** The values as a run's header shows them: "ns=demo-shop · count=2". */
fun runbookValuesLabel(values: Map<String, String>): String = values.entries.joinToString(" · ") { "${it.key}=${it.value}" }

/** What a step acts on: a Kubernetes object, or a node. */
@Serializable
data class RunbookTarget(val kind: String = "", val namespace: String = "", val name: String = "", val node: String = "") {
    val isNode: Boolean get() = node.isNotEmpty()

    /** "Deployment demo-shop/ingest-api", or the node. */
    val label: String get() = if (isNode) node else listOf(kind, "$namespace/$name").filter { it.isNotBlank() }.joinToString(" ")
}

/** One step of RunbookPlan: what it does, and whether the role and the cluster's RBAC allow it. */
@Serializable
data class RunbookPlanStep(
    val id: String,
    val action: String,
    val target: RunbookTarget = RunbookTarget(),
    val destructive: Boolean = false,
    val continueOnError: Boolean = false,
    val role: String = "",
    val allowed: Boolean = true,
    val denied: String = "",
)

@Serializable
data class RunbookPlan(
    val name: String = "",
    val description: String = "",
    val steps: List<RunbookPlanStep> = emptyList(),
    val errors: List<String> = emptyList(),
) {
    /** The steps that would stop the run: refused, and not let past. */
    fun blocking(continueIds: Set<String>): List<RunbookPlanStep> =
        steps.filter { !it.allowed && !it.continueOnError && it.id !in continueIds }

    fun canRun(continueIds: Set<String>): Boolean = steps.isNotEmpty() && blocking(continueIds).isEmpty()
}

/** A step's state while the runbook runs (RunbookListener.OnStep). */
@Serializable
data class RunbookStepState(
    val index: Int = 0,
    val id: String = "",
    val state: String = "",
    val message: String = "",
    val startedAt: Long = 0,
    val endedAt: Long = 0,
) {
    val waiting: Boolean get() = state == WAITING
    val ended: Boolean get() = state == OK || state == FAILED || state == SKIPPED

    companion object {
        const val WAITING = "waiting"
        const val RUNNING = "running"
        const val OK = "ok"
        const val FAILED = "failed"
        const val SKIPPED = "skipped"
    }
}

/** The run's end (RunbookListener.OnDone). */
@Serializable
data class RunbookResult(val ok: Boolean = false, val steps: List<RunbookStepState> = emptyList())

/** How a destructive step is confirmed: a node's hostname typed, or a plain confirmation. */
fun RunbookPlanStep.confirmsByHostname(): Boolean = target.isNode
