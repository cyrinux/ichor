package name.levis.ichor.ui.dataservices

import androidx.lifecycle.viewModelScope
import name.levis.ichor.data.DATA_SERVICES
import name.levis.ichor.data.INVENTORY
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.dataServiceHints
import name.levis.ichor.ui.LoadingViewModel

/**
 * Longhorn, Garage and CloudNativePG health, for the overview card and the Data services
 * screen. Reading it lists custom resources and runs the Garage CLI in a pod: loaded on
 * demand, never polled.
 */
class DataServicesViewModel(private val dataServices: DataServicesRepository) : LoadingViewModel<DataServices>() {
    override fun cached(): TalosRepository.Timed<DataServices>? = dataServices.cached(DATA_SERVICES)
    override val restores get() = dataServices.restores
    override suspend fun fetch() = dataServices.dataServices(hints)

    /** Garage maintenance; a change shows in a fresh reading (tranquility, repairs running). */
    val garage = GarageActions(viewModelScope, dataServices, onChanged = { refresh() })

    /** Longhorn volume and node actions; their progress shows in a fresh reading. */
    val longhorn = LonghornActions(viewModelScope, dataServices, onChanged = { refresh() })

    /** Certificate details and forced renewals; an issuance shows in a fresh reading. */
    val certificates = CertificateActions(viewModelScope, dataServices, onChanged = { refresh() })

    /** On-demand CloudNativePG backups; the new backup shows in a fresh reading once done. */
    val cnpg = CnpgActions(viewModelScope, dataServices, onChanged = { refresh() })

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
    fun inventoryHints(): String = dataServices.cached<Inventory>(INVENTORY)?.value?.dataServiceHints().orEmpty()
}
