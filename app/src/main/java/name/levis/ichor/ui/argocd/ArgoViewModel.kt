package name.levis.ichor.ui.argocd

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.ARGO_CD
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoProject
import name.levis.ichor.model.ArgoResource
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.projectsToClear
import name.levis.ichor.ui.PolledStatusViewModel
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.workloads.WorkloadRestarts
import name.levis.ichor.data.KUBE_WATCH_RETRY_MILLIS
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.watchForever
import name.levis.ichor.model.ARGO_WATCH_KINDS

/** How an action on [count] apps ended: [failed] of them with [error] (the first one's), [app] when only one. */
data class ArgoActionResult(val action: ArgoAction, val count: Int, val app: String?, val failed: Int, val error: UiText?)

/** How a change to a project's sync windows ended: [error] when refused. */
data class ArgoFreezeResult(val action: ArgoFreezeAction, val error: UiText?)

/**
 * Argo CD for the overview card, the apps screen, the app detail and the app sheet: loads on
 * demand from the cached result first, polls quietly (no refresh indicator) while a sync runs
 * or right after an action, and runs actions with their outcome as one-shot [results].
 */
class ArgoViewModel(
    gitOps: GitOpsRepository,
    kube: KubeRepository,
    /** Called as a load starts; what it returns gets the status loaded (the freeze reminders). */
    private val onLoad: () -> (ArgoStatus) -> Unit = { {} },
) : PolledStatusViewModel<ArgoStatus>(gitOps, kube, ARGO_CD) {
    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the apps whose action request is in flight. */
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    // A queue, not a state: two actions finishing together each get their message.
    private val _results = Channel<ArgoActionResult>(Channel.BUFFERED)
    val results: Flow<ArgoActionResult> = _results.receiveAsFlow()

    private val _freezeResults = Channel<ArgoFreezeResult>(Channel.BUFFERED)
    val freezeResults: Flow<ArgoFreezeResult> = _freezeResults.receiveAsFlow()

    /** Projects whose ended freezes were already cleared (or tried) this time round. */
    private val cleared = mutableSetOf<String>()

    /** Rollout restarts of the app's workloads, followed like an action. */
    val restarts = WorkloadRestarts(viewModelScope, kube) { boost() }

    // Another cluster, or a new configuration.
    override fun onNewSource() = cleared.clear()

    /**
     * Reads the status again while called (the screen is visible) each time an Application
     * changes, instead of waiting for the next poll: the Go core signals at most every 2 s,
     * never for the list it reads at the start; skipped while a read runs. A watch that ends
     * (no Argo CD, a refusal) is followed again after [KUBE_WATCH_RETRY_MILLIS].
     */
    suspend fun follow() {
        watchForever(start = { kube.changeWatch(null, ARGO_WATCH_KINDS) }) { item ->
            if (item is StreamItem.Item) poll()
        }
    }

    override fun fetcher(): suspend () -> ArgoStatus {
        val onStatus = onLoad()
        return {
            gitOps.argoCD().also { status ->
                onStatus(status)
                clearExpired(status)
            }
        }
    }

    /** A sync is running. */
    override fun isBusy(data: ArgoStatus): Boolean = data.apps.any { it.isRunning }

    /** Runs [action] on every app of [apps], one after the other, then follows the outcome. */
    fun act(apps: List<ArgoApp>, action: ArgoAction, options: ArgoSyncOptions? = null) {
        val keys = apps.map { it.key }.toSet()
        if (keys.isEmpty() || keys.any { it in _busy.value }) return
        _busy.update { it + keys }
        viewModelScope.launch {
            val errors = apps.mapNotNull { app ->
                runCatching { gitOps.argoAction(app, action, options) }.exceptionOrNull()
                    ?.takeUnless { it is CancellationException }?.uiText()
            }
            _busy.update { it - keys }
            _results.send(ArgoActionResult(action, apps.size, apps.singleOrNull()?.name, errors.size, errors.firstOrNull()))
            if (errors.size < apps.size) boost()
        }
    }

    /**
     * Changes the sync windows of [project] (freeze, extend, unfreeze, clear ended freezes) once
     * per [options], one after the other, then follows the outcome; [syncAfter] is synced once
     * the change is in (ending a freeze with the fix committed).
     */
    fun freeze(project: ArgoProject, action: ArgoFreezeAction, options: List<ArgoFreezeOptions>, syncAfter: ArgoApp? = null) {
        val key = FREEZE_BUSY + project.key
        if (key in _busy.value || options.isEmpty()) return
        _busy.update { it + key }
        viewModelScope.launch {
            val error = options.firstNotNullOfOrNull { o ->
                runCatching { gitOps.argoFreeze(project.namespace, project.name, action, o) }.exceptionOrNull()
                    ?.takeUnless { it is CancellationException }?.uiText()
            }
            _busy.update { it - key }
            _freezeResults.send(ArgoFreezeResult(action, error))
            if (error == null && syncAfter != null) act(listOf(syncAfter), ArgoAction.SYNC) else if (error == null) boost()
        }
    }

    /** Whether a change to [project]'s windows is in flight. */
    fun freezeBusy(busy: Set<String>, project: ArgoProject?): Boolean = project != null && FREEZE_BUSY + project.key in busy

    /**
     * Ichor's ended freezes would fire again a year later (Argo CD has no one-shot window):
     * removed quietly, once per project; a refusal (a read-only role, the demo) is left alone.
     */
    private fun clearExpired(status: ArgoStatus) {
        status.projectsToClear.filter { FREEZE_BUSY + it.key !in _busy.value && cleared.add(it.key) }.forEach { project ->
            viewModelScope.launch {
                runCatching { gitOps.argoFreeze(project.namespace, project.name, ArgoFreezeAction.CLEAR_EXPIRED) }
            }
        }
    }

    /**
     * The workload behind [resource] at once, from a Kubernetes list when one holds it (its
     * replicas matter to the confirmation); [WorkloadRestarts.current] reads it fresh.
     */
    fun workloadFor(resource: ArgoResource): KubeWorkload =
        workloadOf(resource.kind, resource.namespace, resource.name)

    private companion object {
        /** Prefix of a project's key in [busy]. */
        const val FREEZE_BUSY = "freeze:"
    }
}
