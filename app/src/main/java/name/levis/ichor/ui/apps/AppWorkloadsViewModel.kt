package name.levis.ichor.ui.apps

import androidx.lifecycle.viewModelScope
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.ref
import name.levis.ichor.model.routePods
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.workloads.WorkloadRestarts

/**
 * The Deployments, StatefulSets and DaemonSets running the app of the open detail sheet, to
 * restart them from there. Asks the Kubernetes API (os:admin) for its pods' owners: only
 * those pods and workloads are read, never a cluster-wide list.
 */
class AppWorkloadsViewModel(private val kube: KubeRepository) : LoadingViewModel<List<KubeWorkload>>() {
    private var app: InventoryApp? = null

    /**
     * The workloads found for [app]: a restart replaces its pods, so after one the inventory's
     * pod names no longer lead to their workloads; only their state is fetched again.
     */
    private var found: List<KubeWorkload>? = null

    override suspend fun fetch(): List<KubeWorkload> {
        // One deleted since drops out.
        found?.let { known -> return kube.workloadsNamed(known.map { it.ref }) }
        val pods = app?.routePods.orEmpty()
        if (pods.isEmpty()) return emptyList()
        return kube.appWorkloads(pods).also { found = it }
    }

    // Show the rollout starting: the controller already bumped the generation.
    val restarts = WorkloadRestarts(viewModelScope, kube) { refresh() }

    /** Loads the workloads of [app] unless they are already loaded for it (the same pods too). */
    fun load(app: InventoryApp) {
        if (app == this.app) return
        this.app = app
        found = null
        refresh(reset = true)
    }
}
