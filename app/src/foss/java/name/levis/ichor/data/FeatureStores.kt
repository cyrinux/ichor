package name.levis.ichor.data

import android.app.Activity
import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** The open-source builds sell nothing: support goes through the donation links instead. */
@Suppress("UNUSED_PARAMETER")
fun createFeatureStore(context: Context, history: FundingHistory): FeatureStore = NoFeatureStore

private object NoFeatureStore : FeatureStore {
    override val state: StateFlow<StoreState> = MutableStateFlow(StoreState.Unavailable).asStateFlow()
    override val result: StateFlow<PurchaseResult?> = MutableStateFlow(null)

    override suspend fun load(productIds: List<String>) = Unit

    override suspend fun finishPurchases() = Unit

    override fun buy(activity: Activity, productId: String) = Unit

    override fun dismiss() = Unit
}
