package name.levis.ichor.ui.workloads

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.PagedLoad
import name.levis.ichor.model.eagerLimit
import name.levis.ichor.model.isKubeListExpired
import name.levis.ichor.model.loadPages
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.uiText

/**
 * A Kubernetes list of the screen's [scope], loaded page by page (plans/roadmap/large-clusters.md):
 * eagerly up to [eagerLimit] rows (fewer when [metered]), rows shown as they arrive until the
 * first load completes, then further pages on scroll ([loadMore]) in the server's order. A
 * refresh keeps the rows on screen until the new load completes; an expired list starts again
 * silently; only a complete list is kept as the last known one.
 */
abstract class PagedListViewModel<T>(
    protected val talos: TalosRepository,
    private val metered: () -> Boolean,
) : LoadingViewModel<PagedLoad<T>>() {
    override val keepsDataOnFailure = true
    override val restores get() = talos.restores

    /** The namespace listed; set by the screen ([setScope]). */
    var scope: KubeScope = KubeScope()
        private set

    private val _progress = MutableStateFlow<PagedLoad<T>?>(null)
    /** The load in flight, for its progress bar; null when none runs. */
    val progress: StateFlow<PagedLoad<T>?> = _progress.asStateFlow()

    private var more: Job? = null

    /** The cache key of [namespace]'s complete list (see [TalosRepository.keeper]). */
    protected abstract fun key(namespace: String?): String

    /** One page of [namespace] ("" [token] for the first). */
    protected abstract suspend fun page(namespace: String?, token: String): KubePage<T>

    override fun cached(): TalosRepository.Timed<PagedLoad<T>>? =
        talos.cached<List<T>>(key(scope.namespace))?.let { TalosRepository.Timed(PagedLoad.complete(it.value), it.at) }

    /** Lists [scope] from now on; loads it unless it is already what is shown. */
    fun setScope(scope: KubeScope) {
        if (scope == this.scope && state.value != UiState.Loading) return
        val changed = scope != this.scope
        this.scope = scope
        more?.cancel()
        refresh(reset = changed)
    }

    override suspend fun fetch(): PagedLoad<T> {
        val scope = scope
        val keep = talos.keeper(key(scope.namespace))
        try {
            val load = loadPages(eagerLimit(scope, metered()), { token -> page(scope.namespace, token) }) { partial ->
                _progress.value = partial
                showPartial(partial)
            }
            if (load.done) keep(load.items)
            return load
        } finally {
            _progress.value = null
        }
    }

    /** Loads the next page of a list that stopped at its cap, when the user scrolled near its end. */
    fun loadMore() {
        val loaded = state.value as? UiState.Loaded ?: return
        val current = loaded.data
        if (!current.hasMore || loaded.refreshing || more?.isActive == true) return
        val scope = scope
        more = viewModelScope.launch {
            try {
                replaceLoaded(current, current.loadMore { token -> page(scope.namespace, token) })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // An expired list starts again from its first page (L12).
                if (e.isKubeListExpired()) refresh() else replaceLoaded(current, current, e.uiText())
            }
        }
    }
}
