package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * A runbook the model wrote from the diagnosis answer (Go's Diagnosis.ProposeRunbook): its
 * [runbookYAML] and [plan] as the planner checked them, and the [rejected] steps the app cannot
 * run. [empty]: no step was left, [runbookYAML] is then "" and [plan] null.
 */
@Serializable
data class RunbookProposal(
    val runbookYAML: String = "",
    val plan: RunbookPlan? = null,
    val rejected: List<RunbookRejected> = emptyList(),
    val empty: Boolean = false,
)

/** A step the model proposed that the app cannot run, and why. [id] is empty when the whole block was unreadable. */
@Serializable
data class RunbookRejected(val id: String = "", val reason: String = "")
