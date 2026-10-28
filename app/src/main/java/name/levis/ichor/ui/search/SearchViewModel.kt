package name.levis.ichor.ui.search

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.mapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import name.levis.ichor.data.ARGO_CD
import name.levis.ichor.data.FLUX
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.SearchRepository
import name.levis.ichor.data.SearchSnapshot
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.data.cronJobsKey
import name.levis.ichor.data.podsKey
import name.levis.ichor.data.workloadsKey
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.ScreenEntry
import name.levis.ichor.model.ScreenMatches
import name.levis.ichor.model.SearchResults
import name.levis.ichor.model.eagerLimit
import name.levis.ichor.model.fetchWorkloadPage
import name.levis.ichor.model.loadPages

/**
 * What the search screen shows: [results] of [query] once [snapshot] was read (null before),
 * and the active cluster's [screens] that match it.
 */
data class SearchState(
    val snapshot: SearchSnapshot? = null,
    val query: String = "",
    val results: SearchResults = SearchResults(),
    val screens: ScreenMatches = ScreenMatches(),
)

/**
 * Search across clusters: reads what every cluster last showed once ([SearchRepository.snapshot]),
 * then ranks it as the user types, 150 ms after the last key. Only [refreshActive] calls a cluster.
 */
@OptIn(FlowPreview::class, ExperimentalCoroutinesApi::class)
class SearchViewModel(
    private val search: SearchRepository,
    private val prefs: UiPreferences,
    private val kube: KubeRepository,
    private val gitOps: GitOpsRepository,
    private val metered: () -> Boolean,
) : ViewModel() {
    private val snapshot = MutableStateFlow<SearchSnapshot?>(null)
    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    val recent: StateFlow<List<String>> = prefs.recentSearches

    private val _refreshing = MutableStateFlow(false)
    val refreshing: StateFlow<Boolean> = _refreshing.asStateFlow()

    private val _refreshFailed = MutableStateFlow(false)
    /** The last refresh read nothing: the cluster could not be reached or refused. */
    val refreshFailed: StateFlow<Boolean> = _refreshFailed.asStateFlow()

    private val catalog = MutableStateFlow<List<ScreenEntry>>(emptyList())

    val state: StateFlow<SearchState> = combine(snapshot.filterNotNull(), _query.debounce(DEBOUNCE_MS), catalog) { s, q, c -> Triple(s, q, c) }
        .mapLatest { (s, q, c) -> SearchState(s, q, rank(s, q), rankScreens(c, q)) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SearchState())

    init {
        viewModelScope.launch { snapshot.value = readSnapshot() }
    }

    /** The active cluster's screens, titles resolved (see screenCatalog). */
    fun setCatalog(screens: List<ScreenEntry>) {
        catalog.value = screens
    }

    fun setQuery(query: String) {
        _query.value = query
    }

    /** Keeps the query among the recent searches (the user searched it or opened a result). */
    fun remember(query: String = _query.value) = prefs.addRecentSearch(query)

    fun forget(query: String) = prefs.removeRecentSearch(query)

    /**
     * Reads the active cluster's pods, workloads, CronJobs and GitOps apps again, then searches
     * them: the only cluster call of the screen. A list not loaded completely is searched but
     * not kept (the last known state is never a partial one).
     */
    fun refreshActive() {
        if (_refreshing.value) return
        viewModelScope.launch {
            _refreshing.value = true
            try {
                val fresh = fetchActive()
                _refreshFailed.value = fresh == null
                snapshot.value = readSnapshot(fresh.orEmpty())
            } finally {
                _refreshing.value = false
            }
        }
    }

    private suspend fun readSnapshot(fresh: Map<String, Any> = emptyMap()): SearchSnapshot =
        try {
            search.snapshot(fresh)
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            SearchSnapshot.EMPTY
        }

    private suspend fun rank(snapshot: SearchSnapshot, query: String): SearchResults =
        try {
            search.search(snapshot, query)
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            SearchResults()
        }

    private suspend fun rankScreens(screens: List<ScreenEntry>, query: String): ScreenMatches =
        try {
            search.screens(screens, query)
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            ScreenMatches()
        }

    /** The fresh lists by cache key; null when none could be read. */
    private suspend fun fetchActive(): Map<String, Any>? = coroutineScope {
        val pods = async { attempt { list(podsKey(null)) { token -> kube.podsPage(null, token, table = token.isNotEmpty()) } } }
        val workloads = async {
            attempt { list(workloadsKey(null)) { token -> fetchWorkloadPage(token) { kind, kindToken -> kube.workloadsPage(kind, null, kindToken) } } }
        }
        val cronJobs = async { attempt { list(cronJobsKey(null)) { token -> kube.cronJobsPage(null, token) } } }
        // Remembered by their repository: the snapshot reads them from memory.
        val argo = async { if (installed<ArgoStatus>(ARGO_CD) { it.installed }) attempt { gitOps.argoCD() } else null }
        val flux = async { if (installed<FluxStatus>(FLUX) { it.installed }) attempt { gitOps.flux() } else null }
        val lists = listOfNotNull(pods.await(), workloads.await(), cronJobs.await())
        val any = lists.isNotEmpty() || listOf(argo.await(), flux.await()).any { it != null }
        if (any) lists.toMap() else null
    }

    /** Every page of [key]'s list up to the eager cap, kept when complete. */
    private suspend fun <T> list(key: String, page: suspend (String) -> KubePage<T>): Pair<String, List<T>> {
        val keep = kube.keeper(key)
        val load = loadPages(eagerLimit(KubeScope(), metered()), page)
        if (load.done) keep(load.items)
        return key to load.items
    }

    /** Whether to ask for Argo CD or Flux: not when the cluster already answered it has none. */
    private fun <T> installed(key: String, installed: (T) -> Boolean): Boolean = kube.cached<T>(key)?.value?.let(installed) ?: true

    private suspend fun <T> attempt(block: suspend () -> T): T? =
        try {
            block()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        }

    private companion object {
        const val DEBOUNCE_MS = 150L
    }
}
