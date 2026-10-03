package name.levis.ichor.ui.apps

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.ownersOf
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.workloads.WorkloadRestarts

/**
 * The Deployments, StatefulSets and DaemonSets running the app of the open detail sheet, to
 * restart them from there. Asks the Kubernetes API (os:admin) for its pods' owners.
 */
class AppWorkloadsViewModel(private val talos: TalosRepository) : LoadingViewModel<List<KubeWorkload>>() {
    private var app: InventoryApp? = null

    /**
     * The keys found for [app]: a restart replaces its pods, so after one the inventory's pod
     * names no longer lead to their workloads; only their state is fetched again.
     */
    private var keys: Set<String>? = null

    override suspend fun fetch(): List<KubeWorkload> {
        keys?.let { known -> return talos.workloads().filter { it.key in known } }
        val pods = app?.pods.orEmpty()
        if (pods.isEmpty()) return emptyList()
        val owners = coroutineScope {
            val kubePods = async { talos.pods() }
            val workloads = async { talos.workloads() }
            workloads.await().ownersOf(pods, kubePods.await())
        }
        keys = owners.map { it.key }.toSet()
        return owners
    }

    // Show the rollout starting: the controller already bumped the generation.
    val restarts = WorkloadRestarts(viewModelScope, talos) { refresh() }

    /** Loads the workloads of [app] unless they are already loaded for it (the same pods too). */
    fun load(app: InventoryApp) {
        if (app == this.app) return
        this.app = app
        keys = null
        refresh(reset = true)
    }
}
