package name.levis.ichor.ui.gitops

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.MutableStateFlow
import name.levis.ichor.R
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.model.GitProposalRecord
import name.levis.ichor.model.GitRequestState
import name.levis.ichor.model.ProposalStatus
import name.levis.ichor.model.RevisionRuns
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.proposalStatus
import name.levis.ichor.model.shortRevision
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.timeAgo

/** What one proposal's row knows: the forge's answer and what the app runs, or why not. */
private data class ProposalView(val state: GitRequestState? = null, val runs: RevisionRuns? = null, val error: UiText? = null)

/**
 * The pull requests opened from the phone for [tool]'s [kind] [namespace]/[name] ("" for the
 * cluster repository): each with its state on the forge and, once merged, whether the app runs
 * it; [onReconcile] (null: none) applies a merged one now. Read again each time [refresh] changes.
 * Nothing when there is none.
 */
@Composable
fun ProposalsCard(
    repository: GitOpsRepository,
    tool: String,
    kind: String,
    namespace: String,
    name: String,
    refresh: Any? = null,
    onReconcile: (() -> Unit)? = null,
) {
    // Recomposes when a proposal is added or forgotten.
    val all by (repository.proposalsFlow ?: remember { MutableStateFlow(emptyMap()) }).collectAsState()
    val records = remember(all, tool, kind, namespace, name) { repository.proposalsFor(tool, kind, namespace, name) }
    if (records.isEmpty()) return
    val views = remember { mutableStateMapOf<String, ProposalView>() }
    LaunchedEffect(records, refresh) {
        records.forEach { r ->
            views[r.key] = cancellableCatching {
                val state = repository.requestState(r)
                ProposalView(state, if (state.merged) cancellableCatching { repository.revisionRuns(r, state) }.getOrNull() else null)
            }.getOrElse { ProposalView(error = it.uiText()) }
        }
    }
    val uri = LocalUriHandler.current

    SectionCard(stringResource(R.string.proposals_title)) {
        records.forEach { r ->
            val view = views[r.key]
            val status = proposalStatus(view?.state, view?.runs)
            ResourceRow(
                health = status.health(),
                title = view?.state?.title?.ifEmpty { null } ?: r.title.ifEmpty { r.path },
                subtitle = listOfNotNull("#${r.number}", stringResource(status.label()), view?.runs?.revision?.takeIf { status == ProposalStatus.OTHER_REVISION }?.let(::shortRevision)).joinToString(" · "),
                mono = false,
                onClick = r.url.takeIf { it.isNotEmpty() }?.let { url -> { runCatching { uri.openUri(url) } } },
                trailing = {
                    Tag(stringResource(status.label()))
                    TooltipIconButton(Icons.Outlined.Close, stringResource(R.string.proposals_forget), onClick = { repository.forgetProposal(r) })
                },
            ) {
                view?.error?.let { MutedText(it.asString()) }
                // The forge cannot be read now: what the background monitor last saw of it.
                if (view?.error != null && r.stateAt > 0) {
                    when (r.state) {
                        GitRequestState.STATE_MERGED -> MutedText(stringResource(R.string.proposals_merged_ago, timeAgo(r.stateAt)))
                        GitRequestState.STATE_CLOSED -> MutedText(stringResource(R.string.proposals_closed_ago, timeAgo(r.stateAt)))
                    }
                }
                if (onReconcile != null && (status == ProposalStatus.MERGED || status == ProposalStatus.OTHER_REVISION)) {
                    MutedText(stringResource(if (status == ProposalStatus.MERGED) R.string.proposals_not_deployed else R.string.proposals_other_revision))
                    Row(Modifier.padding(top = 4.dp)) {
                        IchorOutlinedButton(onClick = onReconcile) { Text(stringResource(R.string.proposals_reconcile)) }
                    }
                }
            }
        }
    }
}

private fun ProposalStatus.label(): Int = when (this) {
    ProposalStatus.OPEN -> R.string.proposals_open
    ProposalStatus.DRAFT -> R.string.proposals_draft
    ProposalStatus.CLOSED -> R.string.proposals_closed
    ProposalStatus.MERGED -> R.string.proposals_merged
    ProposalStatus.DEPLOYED -> R.string.proposals_deployed
    ProposalStatus.OTHER_REVISION -> R.string.proposals_merged
    ProposalStatus.UNKNOWN -> R.string.proposals_unknown
}

private fun ProposalStatus.health(): ServiceHealth = when (this) {
    ProposalStatus.DEPLOYED -> ServiceHealth.OK
    ProposalStatus.MERGED, ProposalStatus.OTHER_REVISION -> ServiceHealth.WARNING
    ProposalStatus.OPEN, ProposalStatus.DRAFT, ProposalStatus.CLOSED -> ServiceHealth.IDLE
    ProposalStatus.UNKNOWN -> ServiceHealth.UNKNOWN
}
