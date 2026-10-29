package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import name.levis.ichor.model.ClusterUpdates
import name.levis.ichor.model.UpdateEditTarget
import name.levis.ichor.model.UpdateProposalPlan
import name.levis.ichor.model.UpdateRow
import name.levis.ichor.model.gitHostJsonLacksToken
import name.levis.ichor.model.updateItemJson
import name.levis.ichor.model.updateProposalItemsJson
import name.levis.ichor.model.updateSections
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichorgo.Ichorgo

/**
 * The active cluster's update candidates (`ClusterUpdates`): its images and charts with their
 * newer versions, asked of the registries. The last answer is kept in memory per cluster for
 * the session ([outdated] feeds the GitOps card's count); nothing runs in the background.
 */
class UpdatesRepository(
    go: GoCall,
    private val gitOps: GitOpsRepository,
    private val gitHosts: GitHostStore?,
    private val consent: UpdateLookupConsent?,
) : GoRepository(go) {
    private val _outdated = MutableStateFlow<Map<String, Int>>(emptyMap())

    /** The number of outdated images and charts of each cluster (by context) looked up this session. */
    val outdated: StateFlow<Map<String, Int>> = _outdated.asStateFlow()

    private val last = mutableMapOf<String, ClusterUpdates>()

    /** The key of the cluster on screen in [outdated]. */
    fun clusterKey(): String = go.configs.config.value?.let { it.activeSummary?.fingerprint?.ifBlank { null } ?: it.activeContext }.orEmpty()

    /** The last answer for the cluster on screen, this session; null before a lookup. */
    fun lastResult(): ClusterUpdates? = synchronized(last) { last[clusterKey()] }

    /** Whether the one-time "the app asks the registries" notice was accepted for the cluster on screen. */
    fun consented(): Boolean = consent?.fingerprints?.value?.contains(clusterKey()) ?: true

    fun consent() {
        consent?.set(clusterKey(), true)
    }

    /**
     * The cluster's images and charts with their newer versions (os:admin, read only): the
     * cluster first, then the registries (10 to 20 s for 100 images the first time). With a
     * saved cluster repository and its token, a dependency bot found there marks what it bumps.
     */
    suspend fun clusterUpdates(): ClusterUpdates {
        val key = clusterKey()
        val (host, options) = repoArgs()
        val result = go.kube { cfg, ctx, server -> TalosJson.decodeFromString(ClusterUpdates.serializer(), Ichorgo.clusterUpdates(cfg, ctx, server, host, options)) }
        synchronized(last) { last[key] = result }
        _outdated.update { it + (key to updateSections(result).outdated) }
        return result
    }

    /** Where [row] is edited in Git for [version] (`UpdateEditTarget`), read only. */
    suspend fun editTarget(row: UpdateRow, version: String): UpdateEditTarget {
        val (host, _) = repoArgs()
        val item = updateItemJson(row, version)
        return go.kube { cfg, ctx, server -> TalosJson.decodeFromString(UpdateEditTarget.serializer(), Ichorgo.updateEditTarget(cfg, ctx, server, host, item)) }
    }

    /**
     * One pull request for the [selection] (rows and the versions picked, `UpdateProposalPlan`):
     * each file read at the request's branch with every edit applied, the title, the body's
     * table, and the rows left out with why. Read only.
     */
    suspend fun proposalPlan(selection: List<Pair<UpdateRow, String>>): UpdateProposalPlan {
        val (host, _) = repoArgs()
        val items = updateProposalItemsJson(selection)
        return go.kube { cfg, ctx, server -> TalosJson.decodeFromString(UpdateProposalPlan.serializer(), Ichorgo.updateProposalPlan(cfg, ctx, server, host, items)) }
    }

    /** The saved cluster repository's host (with its token) and ClusterUpdates' options naming it; blank without them. */
    private suspend fun repoArgs(): Pair<String, String> {
        val repo = gitOps.activeClusterRepo() ?: return "" to ""
        val store = gitHosts ?: return "" to ""
        val host = cancellableCatching { store.hostJson(store.hostFor(repo.host)) }.getOrNull()?.takeUnless(::gitHostJsonLacksToken) ?: return "" to ""
        return host to buildJsonObject {
            put("repoURL", repo.repoURL)
            put("ref", repo.branch)
        }.toString()
    }
}

/** The clusters whose registries the user agreed to have asked (the one-time notice of the Updates screen). */
class UpdateLookupConsent(prefs: SharedPreferences) : FingerprintSet(prefs) {
    companion object {
        const val FILE = "ichor-update-lookups"
    }
}
