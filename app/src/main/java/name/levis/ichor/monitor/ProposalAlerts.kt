package name.levis.ichor.monitor

import kotlinx.coroutines.CancellationException
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.GitProposalRecord
import name.levis.ichor.model.GitRequestState
import name.levis.ichor.model.ProposalChange
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.proposalStateChanges
import name.levis.ichor.model.proposalsToCheck

/** At most this many requests a cluster's run asks one forge host about: one GitForgeRequestStates call. */
const val PROPOSAL_CHECKS_PER_HOST = 20

/**
 * The "merged" / "closed" alerts of the pull requests opened from the phone on [contexts] (the
 * clusters this run read): their still-open requests are read on their forge, one call per Git
 * host and cluster, and the new states kept on the records (so each change is told once). A
 * host without a token, or a forge that does not answer, tells nothing and keeps the states.
 */
suspend fun proposalAlerts(app: TalosApp, contexts: List<ContextSummary>, now: Long): List<ClusterAlerts> =
    contexts.filterNot { it.isDemo || it.fingerprint.isBlank() }.mapNotNull { context ->
        val records = app.proposals.proposals.value[context.fingerprint].orEmpty()
        val asked = proposalsToCheck(records).groupBy { it.host }.flatMap { (host, list) ->
            val first = list.take(PROPOSAL_CHECKS_PER_HOST)
            try {
                app.gitOpsRepository.requestStates(host, first)
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                emptyList()
            }
        }
        if (asked.isEmpty()) return@mapNotNull null
        // The records as they are now: one may have been added or forgotten during the reads.
        val (next, changes) = proposalStateChanges(app.proposals.proposals.value[context.fingerprint].orEmpty(), asked, now)
        app.proposals.replace(context.fingerprint, next)
        changes.takeIf { it.isNotEmpty() }?.let { ClusterAlerts(context, it.map(::proposalAlert)) }
    }

/**
 * The alert of a request [change]: key "proposal:<repoURL>#<number>", subject its title, detail
 * "number|tool|kind|namespace|name" (what the notification names and opens).
 */
fun proposalAlert(change: ProposalChange): Alert {
    val r = change.record
    return Alert(
        key = "proposal:${r.key}",
        kind = if (change.state == GitRequestState.STATE_MERGED) AlertKind.PROPOSAL_MERGED else AlertKind.PROPOSAL_CLOSED,
        problem = false,
        subject = r.title,
        detail = listOf(r.number.toString(), r.tool, r.kind, r.namespace, r.name).joinToString("|"),
    )
}

/** What [proposalAlert]'s detail holds. */
data class ProposalDetail(val number: String, val tool: String, val kind: String, val namespace: String, val name: String) {
    companion object {
        fun parse(detail: String): ProposalDetail {
            val p = detail.split('|') + List(5) { "" }
            return ProposalDetail(p[0], p[1], p[2], p[3], p[4])
        }
    }
}

/** The record a [GitProposalRecord] alert came from opens its app's screen; the repository's, the cluster. */
internal fun ProposalDetail.isApp(): Boolean = tool.isNotEmpty() && name.isNotEmpty()
