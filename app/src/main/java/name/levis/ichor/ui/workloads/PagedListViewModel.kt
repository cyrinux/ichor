package name.levis.ichor.ui.workloads

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.KubeRepository
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
 * A Kubernetes list of the screen's [scope], loaded page by page (the Linear plan document "U11. Large clusters: home at scale, namespace-first paged lists"):
 * eagerly up to [eagerLimit] rows (fewer when [metered]), rows shown as they arrive until the
 * first load completes, then further pages on demand ([loadMore], [loadAll]) in the server's
 * order. A refresh keeps the rows on screen until the new load completes; an expired list
 * starts again silently; only a complete list is kept as the last known one.
 */
abstract class PagedListViewModel<T>(
    protected val kube: KubeRepository,
    private val metered: () -> Boolean,
) : LoadingViewModel<PagedLoad<T>>() {
    override val keepsDataOnFailure = true
    override val restores get() = kube.restores

    /** The namespace listed; set by the screen ([setScope]). */
    var scope: KubeScope = KubeScope()
        private set
    private var started = false

    private val _progress = MutableStateFlow<PagedLoad<T>?>(null)
    /** The load in flight, for its progress bar; null when none runs. */
    val progress: StateFlow<PagedLoad<T>?> = _progress.asStateFlow()

    /** The load [progress] belongs to: one superseded must not clear the next one's. */
    private var progressOwner: Any? = null

    private var more: Job? = null

    /** The cache key of [namespace]'s complete list (see [KubeRepository.keeper]). */
    protected abstract fun key(namespace: String?): String

    /** One page of [namespace] ("" [token] for the first). */
    protected abstract suspend fun page(namespace: String?, token: String): KubePage<T>

    /** Rows the first load reads before further pages wait for a scroll. */
    protected open fun eagerRows(scope: KubeScope): Int = eagerLimit(scope, metered())

    /** Whether rows kept from an earlier load carry what only full objects do (see [PagedLoad.detailed]). */
    protected open fun detailed(items: List<T>): Boolean = true

    override fun cached(): TalosRepository.Timed<PagedLoad<T>>? =
        kube.cached<List<T>>(key(scope.namespace))?.let { TalosRepository.Timed(PagedLoad.complete(it.value, detailed(it.value)), it.at) }

    /** Lists [scope] from now on; loads it the first time and when it changes, else nothing. */
    fun setScope(scope: KubeScope) {
        if (scope == this.scope && started) return
        val changed = scope != this.scope
        this.scope = scope
        started = true
        more?.cancel()
        refresh(reset = changed)
    }

    override suspend fun fetch(): PagedLoad<T> {
        val scope = scope
        val keep = kube.keeper(key(scope.namespace))
        val owner = startProgress()
        try {
            val load = loadPages(eagerRows(scope), { token -> page(scope.namespace, token) }) { partial ->
                showProgress(owner, partial)
                showPartial(partial)
            }
            if (load.done) keep(load.items)
            return load
        } finally {
            endProgress(owner)
        }
    }

    /** Loads the next page of a list that stopped at its cap (scrolled near its end, or asked). */
    fun loadMore() = loadFurther(all = false)

    /** Loads every page left of a list that stopped at its cap: the user asked for all of it. */
    fun loadAll() = loadFurther(all = true)

    private fun loadFurther(all: Boolean) {
        val loaded = state.value as? UiState.Loaded ?: return
        val start = loaded.data
        if (!start.hasMore || loaded.refreshing || more?.isActive == true) return
        val scope = scope
        val keep = kube.keeper(key(scope.namespace))
        more = viewModelScope.launch {
            val owner = if (all) startProgress() else null
            var shown = start
            try {
                do {
                    val next = shown.loadMore { token -> page(scope.namespace, token) }
                    // A refresh replaced the list meanwhile: it is the one to follow.
                    if (!replaceLoaded(shown, next)) return@launch
                    shown = next
                    owner?.let { showProgress(it, next) }
                } while (all && shown.hasMore)
                // Complete at last: the last known state from now on (L12).
                if (shown.done) keep(shown.items)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // An expired list starts again from its first page (L12).
                if (e.isKubeListExpired()) refresh() else replaceLoaded(shown, shown, e.uiText())
            } finally {
                owner?.let { endProgress(it) }
            }
        }
    }

    private fun startProgress(): Any = Any().also { progressOwner = it }

    private fun showProgress(owner: Any, load: PagedLoad<T>) {
        if (progressOwner === owner) _progress.value = load
    }

    private fun endProgress(owner: Any) {
        if (progressOwner !== owner) return
        progressOwner = null
        _progress.value = null
    }
}
