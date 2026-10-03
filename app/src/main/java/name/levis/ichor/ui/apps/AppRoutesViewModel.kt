package name.levis.ichor.ui.apps

import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.model.routePods
import name.levis.ichor.ui.LoadingViewModel

/**
 * The URLs the app of the open detail sheet is served at, from the Ingresses and HTTPRoutes
 * pointing to the Services that select its pods (os:admin).
 */
class AppRoutesViewModel(private val talos: TalosRepository) : LoadingViewModel<List<KubeRoute>>() {
    private var app: InventoryApp? = null

    override suspend fun fetch(): List<KubeRoute> {
        val pods = app?.routePods.orEmpty()
        return if (pods.isEmpty()) emptyList() else talos.appRoutes(pods)
    }

    /** Loads the routes of [app] unless they are already loaded for it. */
    fun load(app: InventoryApp) {
        if (app == this.app) return
        this.app = app
        refresh(reset = true)
    }
}
