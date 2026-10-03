package name.levis.ichor.data

import android.app.Activity
import android.content.Context
import com.android.billingclient.api.BillingClient
import com.android.billingclient.api.BillingClientStateListener
import com.android.billingclient.api.BillingFlowParams
import com.android.billingclient.api.BillingResult
import com.android.billingclient.api.ConsumeParams
import com.android.billingclient.api.PendingPurchasesParams
import com.android.billingclient.api.ProductDetails
import com.android.billingclient.api.Purchase
import com.android.billingclient.api.PurchasesUpdatedListener
import com.android.billingclient.api.QueryProductDetailsParams
import com.android.billingclient.api.QueryPurchasesParams
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import name.levis.ichor.R
import name.levis.ichor.ui.UiText
import java.util.concurrent.ConcurrentHashMap
import kotlin.coroutines.resume

/** Google Play Billing: what each product raised shows in the Play Console reports. */
fun createFeatureStore(context: Context, history: FundingHistory): FeatureStore =
    PlayFeatureStore(context.applicationContext, history)

/**
 * Every product is a consumable, consumed as soon as it is paid: it can be bought again, and
 * consuming also acknowledges it (Play refunds purchases left unacknowledged for 3 days). There
 * is nothing to unlock, so purchases are not verified on a server.
 */
private class PlayFeatureStore(context: Context, private val history: FundingHistory) : FeatureStore, PurchasesUpdatedListener {
    private val _state = MutableStateFlow<StoreState>(StoreState.Loading)
    override val state: StateFlow<StoreState> = _state.asStateFlow()

    private val _result = MutableStateFlow<PurchaseResult?>(null)
    override val result: StateFlow<PurchaseResult?> = _result.asStateFlow()

    @Volatile
    private var details: Map<String, ProductDetails> = emptyMap()

    /** Purchase tokens being consumed: the purchase flow and [completeUnfinished] can both see one. */
    private val consuming = ConcurrentHashMap.newKeySet<String>()

    private val connecting = Mutex()

    /** Set once the first connection succeeded; from then on the client reconnects by itself. */
    @Volatile
    private var connected = false

    private val client = BillingClient.newBuilder(context)
        .setListener(this)
        .enablePendingPurchases(PendingPurchasesParams.newBuilder().enableOneTimeProducts().build())
        .enableAutoServiceReconnection()
        .build()

    override suspend fun load(productIds: List<String>) {
        if (!connect()) {
            _state.value = StoreState.Unavailable
            return
        }
        completeUnfinished(announce = false)
        val products = if (productIds.isEmpty()) emptyList() else queryDetails(productIds)
        if (products == null) {
            _state.value = StoreState.Unavailable
            return
        }
        details = products.associateBy { it.productId }
        _state.value = StoreState.Ready(
            products.mapNotNull { product ->
                product.oneTimePurchaseOfferDetails?.let { product.productId to ProductPrice(it.formattedPrice, it.priceAmountMicros) }
            }.toMap(),
        )
    }

    override suspend fun finishPurchases() {
        if (connect()) completeUnfinished(announce = false)
    }

    override fun buy(activity: Activity, productId: String) {
        val product = details[productId] ?: return
        val params = BillingFlowParams.newBuilder()
            .setProductDetailsParamsList(listOf(BillingFlowParams.ProductDetailsParams.newBuilder().setProductDetails(product).build()))
            .build()
        val launched = client.launchBillingFlow(activity, params)
        if (launched.responseCode != BillingClient.BillingResponseCode.OK) fail(launched)
    }

    override fun dismiss() {
        _result.value = null
    }

    override fun onPurchasesUpdated(result: BillingResult, purchases: MutableList<Purchase>?) {
        when (result.responseCode) {
            BillingClient.BillingResponseCode.OK -> purchases.orEmpty().forEach { complete(it, announce = true) }
            BillingClient.BillingResponseCode.USER_CANCELED -> Unit
            // An earlier purchase of it was never consumed (pending, or interrupted): finish it and
            // say how it stands; once consumed, the next try works.
            BillingClient.BillingResponseCode.ITEM_ALREADY_OWNED -> completeUnfinished(announce = true)
            else -> fail(result)
        }
    }

    /** Consumes what was paid but not consumed yet (a pending payment since settled, a crash). */
    private fun completeUnfinished(announce: Boolean) {
        val params = QueryPurchasesParams.newBuilder().setProductType(BillingClient.ProductType.INAPP).build()
        client.queryPurchasesAsync(params) { result, purchases ->
            if (result.responseCode == BillingClient.BillingResponseCode.OK) purchases.forEach { complete(it, announce) }
        }
    }

    /**
     * [announce]: tell the user how it stands (a purchase they just made). Once paid, it is
     * theirs whether or not consuming works now: a failed consume is retried on the next resume.
     */
    private fun complete(purchase: Purchase, announce: Boolean) {
        when (purchase.purchaseState) {
            Purchase.PurchaseState.PURCHASED -> {
                history.record(purchase.products)
                if (announce) _result.value = PurchaseResult.Thanks(purchase.products.firstOrNull().orEmpty())
                val token = purchase.purchaseToken
                if (!consuming.add(token)) return
                client.consumeAsync(ConsumeParams.newBuilder().setPurchaseToken(token).build()) { _, _ -> consuming.remove(token) }
            }
            Purchase.PurchaseState.PENDING -> if (announce) _result.value = PurchaseResult.Pending
            else -> Unit
        }
    }

    private fun fail(result: BillingResult) {
        _result.value = PurchaseResult.Failed(
            result.debugMessage.takeIf { it.isNotBlank() }?.let { UiText.Raw(it) }
                ?: UiText.Res(R.string.funding_billing_error, result.responseCode),
        )
    }

    /** Connects once; later calls wait for that attempt, or retry it if it failed. */
    private suspend fun connect(): Boolean = connecting.withLock {
        if (connected || client.isReady) return true
        connected = suspendCancellableCoroutine { continuation ->
            client.startConnection(object : BillingClientStateListener {
                override fun onBillingSetupFinished(result: BillingResult) {
                    if (continuation.isActive) continuation.resume(result.responseCode == BillingClient.BillingResponseCode.OK)
                }

                override fun onBillingServiceDisconnected() {
                    if (continuation.isActive) continuation.resume(false)
                }
            })
        }
        connected
    }

    /** The products Play sells among [productIds]; null when Play could not be asked. */
    private suspend fun queryDetails(productIds: List<String>): List<ProductDetails>? {
        val params = QueryProductDetailsParams.newBuilder()
            .setProductList(
                productIds.map {
                    QueryProductDetailsParams.Product.newBuilder().setProductId(it).setProductType(BillingClient.ProductType.INAPP).build()
                },
            )
            .build()
        return suspendCancellableCoroutine { continuation ->
            client.queryProductDetailsAsync(params) { result, found ->
                val list = if (result.responseCode == BillingClient.BillingResponseCode.OK) found.productDetailsList else null
                if (continuation.isActive) continuation.resume(list)
            }
        }
    }
}
