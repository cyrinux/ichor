package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

/**
 * A pull or merge request opened from the phone, kept to tell later whether it was merged and
 * whether the cluster runs it: what it changed ([tool]'s [kind] [namespace]/[name], or the
 * cluster repository when [tool] is ""), where ([repoURL], [path]) and what the forge answered.
 * No token: the Git host's is used to read it again.
 */
@Serializable
data class GitProposalRecord(
    val tool: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val repoURL: String = "",
    val path: String = "",
    val title: String = "",
    val url: String = "",
    val number: Int = 0,
    val branch: String = "",
    val commit: String = "",
    val forge: String = "",
    val createdAt: Long = 0,
    /** The request's last known state ([GitRequestState.STATE_OPEN]…), "" when never read; [stateAt] when it was (ms). */
    val state: String = "",
    val stateAt: Long = 0,
) {
    /** One record per request. */
    val key: String get() = "$repoURL#$number"

    /** The Git host whose token reads it ("github.com"). */
    val host: String get() = normalizeGitHost(repoURL)

    /** Made for [target]: the app's proposals, or the repository's. */
    fun isFor(tool: String, kind: String, namespace: String, name: String): Boolean =
        this.tool == tool && this.kind == kind && this.namespace == namespace && this.name == name
}

/** `GitForgeRequest`: the request's state on the forge, its head and merge commits. */
@Serializable
data class GitRequestState(
    val state: String = "",
    val title: String = "",
    val url: String = "",
    val headSha: String = "",
    val mergeSha: String = "",
    val mergedAt: String = "",
    val updatedAt: String = "",
    val draft: Boolean = false,
) {
    val merged: Boolean get() = state == STATE_MERGED
    val open: Boolean get() = state == STATE_OPEN

    companion object {
        const val STATE_OPEN = "open"
        const val STATE_MERGED = "merged"
        const val STATE_CLOSED = "closed"
    }
}

/** One answer of `GitForgeRequestStates`: [state] is empty when [error] says why it could not be read. */
@Serializable
data class GitRequestStateItem(
    val repoURL: String = "",
    val number: Int = 0,
    val state: String = "",
    val mergedAt: String = "",
    val closedAt: String = "",
    val sha: String = "",
    val error: String = "",
) {
    val key: String get() = "$repoURL#$number"
}

/** A request the background monitor saw merged or closed since it was open ([state] the new one). */
data class ProposalChange(val record: GitProposalRecord, val state: String)

/** The proposals the background monitor asks their forge about: still open (or never read), not the demo's. */
fun proposalsToCheck(records: List<GitProposalRecord>): List<GitProposalRecord> =
    records.filter { (it.state.isEmpty() || it.state == GitRequestState.STATE_OPEN) && it.forge != "demo" && it.number > 0 }

/**
 * [records] with the states the forge answered ([items], by request), and the changes to tell:
 * open -> merged or closed. A record never read before ("") takes its state silently, so a request
 * merged before the app could watch it is not announced late; one that could not be read keeps its own.
 */
fun proposalStateChanges(
    records: List<GitProposalRecord>,
    items: List<GitRequestStateItem>,
    nowMillis: Long,
): Pair<List<GitProposalRecord>, List<ProposalChange>> {
    val answered = items.filter { it.error.isEmpty() && it.state.isNotEmpty() }.associateBy { it.key }
    val changes = mutableListOf<ProposalChange>()
    val next = records.map { record ->
        val item = answered[record.key] ?: return@map record
        if (item.state == record.state) return@map record
        val updated = record.copy(state = item.state, stateAt = nowMillis)
        val ended = item.state == GitRequestState.STATE_MERGED || item.state == GitRequestState.STATE_CLOSED
        if (record.state == GitRequestState.STATE_OPEN && ended) changes += ProposalChange(updated, item.state)
        updated
    }
    return next to changes
}

/** `GitOpsRevisionRuns`: whether the app runs a commit, the one it runs, whether it is synced. */
@Serializable
data class RevisionRuns(val runs: Boolean = false, val revision: String = "", val synced: Boolean = false)

/** How a proposal reads on its row. */
enum class ProposalStatus { OPEN, DRAFT, CLOSED, MERGED, DEPLOYED, OTHER_REVISION, UNKNOWN }

/**
 * The row's status from the forge's [state] and, for a merged request, what the app runs
 * ([runs]: null when not asked, for the repository or a failure): Deployed when it runs the
 * merge commit or the head (a squash merge's commit differs); another revision when it runs a
 * commit that is not this one (a newer one, or this one is not applied yet: no commit graph
 * tells which).
 */
fun proposalStatus(state: GitRequestState?, runs: RevisionRuns?): ProposalStatus = when {
    state == null -> ProposalStatus.UNKNOWN
    state.open -> if (state.draft) ProposalStatus.DRAFT else ProposalStatus.OPEN
    !state.merged -> ProposalStatus.CLOSED
    runs == null -> ProposalStatus.MERGED
    runs.runs -> ProposalStatus.DEPLOYED
    runs.revision.isNotEmpty() -> ProposalStatus.OTHER_REVISION
    else -> ProposalStatus.MERGED
}

/** The records kept for one cluster: [record] first, a request kept once, [MAX_PROPOSALS] at most. */
fun withProposal(records: List<GitProposalRecord>, record: GitProposalRecord): List<GitProposalRecord> =
    (listOf(record) + records.filter { it.key != record.key }).take(MAX_PROPOSALS)

/** A request just opened from the phone: open, so the monitor tells when it is merged or closed. */
fun openedProposal(record: GitProposalRecord, nowMillis: Long): GitProposalRecord =
    if (record.state.isEmpty()) record.copy(state = GitRequestState.STATE_OPEN, stateAt = nowMillis) else record

const val MAX_PROPOSALS = 50

private val ProposalJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
}

fun encodeProposals(records: List<GitProposalRecord>): String = ProposalJson.encodeToString(ListSerializer(GitProposalRecord.serializer()), records)

fun decodeProposals(json: String): List<GitProposalRecord>? =
    runCatching { ProposalJson.decodeFromString(ListSerializer(GitProposalRecord.serializer()), json) }.getOrNull()

/** The demo cluster's sample proposals on its Argo CD app argocd/hello-ichor: #1 open, #2 merged and deployed. */
val DEMO_PROPOSALS = listOf(
    GitProposalRecord(
        tool = "argocd", kind = "Application", namespace = "argocd", name = "hello-ichor", repoURL = DEMO_CLUSTER_REPO.repoURL,
        path = "apps/hello-ichor/deployment.yaml", title = "Bump podinfo to 6.7.1", number = 1, forge = "demo",
        url = "https://git.home.example/example-org/cluster-config/pulls/1", createdAt = 1_760_860_800_000,
    ),
    GitProposalRecord(
        tool = "argocd", kind = "Application", namespace = "argocd", name = "hello-ichor", repoURL = DEMO_CLUSTER_REPO.repoURL,
        path = "apps/hello-ichor/deployment.yaml", title = "Scale podinfo to 3", number = 2, forge = "demo",
        url = "https://git.home.example/example-org/cluster-config/pulls/2", createdAt = 1_760_774_400_000,
    ),
)
