package name.levis.ichor.data

import android.app.Activity
import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.ui.UiText

/** A product's price as the store shows it; [micros] orders the tiers. */
data class ProductPrice(val formatted: String, val micros: Long)

sealed interface StoreState {
    data object Loading : StoreState
    /** [prices]: the requested products the store knows; one missing from it is not on sale. */
    data class Ready(val prices: Map<String, ProductPrice>) : StoreState
    /** No store on this device (or the FOSS builds): nothing can be bought. */
    data object Unavailable : StoreState
}

sealed interface PurchaseResult {
    data class Thanks(val productId: String) : PurchaseResult
    /** Paid with a slow method (cash, bank transfer): delivered once the store confirms it. */
    data object Pending : PurchaseResult
    data class Failed(val message: UiText) : PurchaseResult
}

/**
 * In-app purchases that fund features (Google Play Billing in the Play build, nothing in
 * the open-source builds, see `createFeatureStore`). Every product is a consumable: it can be
 * bought again, and unlocks nothing.
 */
interface FeatureStore {
    val state: StateFlow<StoreState>

    /** The outcome of the last purchase until [dismiss]; a cancelled purchase leaves it null. */
    val result: StateFlow<PurchaseResult?>

    /** Prices [productIds], and completes purchases left unfinished (see [finishPurchases]). */
    suspend fun load(productIds: List<String>)

    /**
     * Completes purchases paid while the app was not watching (a pending payment since settled,
     * a crash before completion). Called on every app resume: Play refunds a purchase left
     * unacknowledged for 3 days, whether or not the user reopens the funding screen.
     */
    suspend fun finishPurchases()

    fun buy(activity: Activity, productId: String)

    fun dismiss()
}

/** The products bought on this device, to mark the features the user backed. Never leaves the phone. */
class FundingHistory(private val prefs: SharedPreferences) {
    private val _backed = MutableStateFlow(prefs.getStringSet(KEY_BACKED, null).orEmpty().toSet())
    val backed: StateFlow<Set<String>> = _backed.asStateFlow()

    fun record(productIds: List<String>) {
        val updated = _backed.value + productIds
        prefs.edit().putStringSet(KEY_BACKED, updated).apply()
        _backed.value = updated
    }

    companion object {
        const val FILE = "ichor-funding"
        private const val KEY_BACKED = "backed"
    }
}
