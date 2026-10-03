package name.levis.ichor.ui.apps

import name.levis.ichor.data.INVENTORY
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.Inventory
import name.levis.ichor.ui.LoadingViewModel

/**
 * The cluster's apps, for the Apps screen and the overview card. Listing every node's
 * containers is heavier than a stats sample: loaded on demand, never polled.
 */
class AppsViewModel(private val talos: TalosRepository) : LoadingViewModel<Inventory>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<Inventory>? = talos.cached(INVENTORY)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.inventory()

    private var source: Any? = null

    /**
     * Loads once per [key] (the context, the config generation and the cache invalidations),
     * so coming back to a screen does not list the containers again; [refresh] forces it.
     */
    fun load(key: Any) {
        if (key == source) return
        source = key
        refresh(reset = true)
    }
}
