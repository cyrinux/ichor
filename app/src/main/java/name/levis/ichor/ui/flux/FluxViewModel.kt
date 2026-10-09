package name.levis.ichor.ui.flux

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.data.FLUX
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxResource
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.anyBusy
import name.levis.ichor.model.fluxKey
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.PolledStatusViewModel
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.workloads.WorkloadRestarts

/** How an action on the Flux object [name] ended: [error] when refused. */
data class FluxActionResult(val action: FluxAction, val name: String, val error: UiText?)

/**
 * Flux for the overview card, its screen, an app's detail and the Flux tile's sheet: loads on
 * demand from the cached result first, polls quietly (no refresh indicator) while a controller
 * reconciles or right after an action, and runs actions with their outcome as one-shot [results].
 */
class FluxViewModel(gitOps: GitOpsRepository, kube: KubeRepository) : PolledStatusViewModel<FluxStatus>(gitOps, kube, FLUX) {
    private val actions = KeyedActions<FluxActionResult>(viewModelScope)
    /** Keys ([fluxKey]) of the objects whose action request is in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy

    val results: Flow<FluxActionResult> get() = actions.results

    /** Rollout restarts of a Kustomization's workloads, followed like an action. */
    val restarts = WorkloadRestarts(viewModelScope, kube) { boost() }

    override fun fetcher(): suspend () -> FluxStatus = { gitOps.flux() }

    /** Something reconciles or waits for it. */
    override fun isBusy(data: FluxStatus): Boolean = data.anyBusy

    /** Runs [action] on the object [kind] [namespace]/[name], then follows the outcome. */
    fun act(kind: String, namespace: String, name: String, action: FluxAction) {
        actions.launch(fluxKey(kind, namespace, name), { gitOps.fluxAction(kind, namespace, name, action) }, {
            FluxActionResult(action, name, it.exceptionOrNull()?.uiText())
        }, ::boost)
    }

    /**
     * The workload behind [resource] at once, from a Kubernetes list when one holds it (its
     * replicas matter to the confirmation); [WorkloadRestarts.current] reads it fresh.
     */
    fun workloadFor(resource: FluxResource): KubeWorkload =
        workloadOf(resource.kind, resource.namespace, resource.name)
}
