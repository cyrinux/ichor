package name.levis.ichor.ui.dataservices

import name.levis.ichor.data.DATA_SERVICES
import name.levis.ichor.data.INVENTORY
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.dataServiceHints
import name.levis.ichor.ui.LoadingViewModel

/**
 * Longhorn, Garage and CloudNativePG health, for the overview card and the Data services
 * screen. Reading it lists custom resources and runs the Garage CLI in a pod: loaded on
 * demand, never polled.
 */
class DataServicesViewModel(private val talos: TalosRepository) : LoadingViewModel<DataServices>() {
    override fun cached(): TalosRepository.Timed<DataServices>? = talos.cached(DATA_SERVICES)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.dataServices(hints)

    /** Catalog ids from the inventory; "" checks everything (Garage needs a listing of every pod then). */
    private var hints = ""
    private var source: Any? = null

    /** Loads once per [key] (context, config generation, invalidations); [refresh] forces it. */
    fun load(key: Any, hints: String) {
        if (key == source) return
        source = key
        this.hints = hints
        refresh(reset = true)
    }

    /** Nothing to load (no hint): the next [load], even with a key seen before, loads again. */
    fun forget() {
        source = null
    }

    /** The hints of the inventory the overview already loaded, if any. */
    fun inventoryHints(): String = talos.cached<Inventory>(INVENTORY)?.value?.dataServiceHints().orEmpty()
}
