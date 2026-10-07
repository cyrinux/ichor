package name.levis.ichor.ui.funding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.FeatureStore
import name.levis.ichor.data.ProductPrice
import name.levis.ichor.data.PurchaseResult
import name.levis.ichor.data.RoadmapRepository
import name.levis.ichor.data.StoreState
import name.levis.ichor.model.FeatureStatus
import name.levis.ichor.model.Roadmap
import name.levis.ichor.model.RoadmapFeature
import name.levis.ichor.model.localized
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichor.ui.theme.LocalStatusColors
import java.text.NumberFormat
import java.util.Currency
import java.util.Locale
import name.levis.ichor.ui.components.pageContent

class FundingViewModel(private val roadmap: RoadmapRepository, private val store: FeatureStore) : LoadingViewModel<Roadmap>() {
    override suspend fun fetch(): Roadmap = roadmap.load().also { store.load(it.productIds) }
}

/** Google Play build: back the features to build next with in-app purchases (see [Roadmap]). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FundingScreen(
    onBack: () -> Unit,
    vm: FundingViewModel = viewModel(factory = factory { FundingViewModel(app.roadmapRepository, app.featureStore) }),
) {
    val context = LocalContext.current
    val app = context.applicationContext as name.levis.ichor.TalosApp
    val state by vm.state.collectAsStateWithLifecycle()
    val store by app.featureStore.state.collectAsStateWithLifecycle()
    val result by app.featureStore.result.collectAsStateWithLifecycle()
    val backed by app.fundingHistory.backed.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    result?.let { PurchaseResultDialog(it, app.featureStore::dismiss) }
    val buy: (String) -> Unit = { id -> context.findFragmentActivity()?.let { app.featureStore.buy(it, id) } }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.funding_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, onRetry = { vm.refresh() }, modifier = Modifier.pageContent(padding))
            is UiState.Loaded -> FundingList(s.data, store, backed, buy, Modifier.pageContent(padding))
        }
    }
}

@Composable
private fun FundingList(roadmap: Roadmap, store: StoreState, backed: Set<String>, onBuy: (String) -> Unit, modifier: Modifier) {
    val context = LocalContext.current
    val locale = context.resources.configuration.locales[0]
    val prices = (store as? StoreState.Ready)?.prices.orEmpty()
    LazyColumn(
        modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item {
            Text(stringResource(R.string.funding_intro), style = MaterialTheme.typography.bodyMedium)
            MutedText(
                stringResource(R.string.funding_disclaimer),
                modifier = Modifier.padding(top = 8.dp),
            )
        }
        if (store == StoreState.Unavailable) item { InfoNotice(stringResource(R.string.funding_unavailable)) }
        if (roadmap.features.isEmpty()) item { InfoNotice(stringResource(R.string.funding_empty)) }
        items(roadmap.features, key = { it.id }) { feature ->
            FeatureCard(
                feature = feature,
                locale = locale,
                currency = roadmap.currency,
                prices = prices,
                backed = feature.backedWith(backed),
                onBuy = onBuy,
                onDiscuss = { url -> openUrl(context, url) },
            )
        }
        val tips = priced(roadmap.tips, prices)
        if (tips.isNotEmpty()) {
            item {
                SectionTitle(stringResource(R.string.funding_tips_title))
                MutedText(stringResource(R.string.funding_tips_body))
                PriceButtons(tips, onBuy)
            }
        }
    }
}

@Composable
private fun FeatureCard(
    feature: RoadmapFeature,
    locale: Locale,
    currency: String,
    prices: Map<String, ProductPrice>,
    backed: Boolean,
    onBuy: (String) -> Unit,
    onDiscuss: (String) -> Unit,
) {
    val colors = LocalStatusColors.current
    val lang = locale.language
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(feature.title.localized(lang), style = MaterialTheme.typography.titleMedium)
            when (feature.status) {
                FeatureStatus.IN_PROGRESS -> StatusPill(stringResource(R.string.funding_status_in_progress), colors.warn)
                FeatureStatus.SHIPPED -> StatusPill(
                    feature.shippedIn?.let { stringResource(R.string.funding_shipped_in, it) } ?: stringResource(R.string.funding_status_shipped),
                    colors.ok,
                )
                FeatureStatus.OPEN -> Unit
            }
            feature.description.localized(lang).takeIf { it.isNotBlank() }?.let {
                Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (feature.goal > 0) {
                LinearProgressIndicator(progress = { feature.progress }, modifier = Modifier.fillMaxWidth())
                Text(
                    stringResource(R.string.funding_raised, money(feature.raised, currency, locale), money(feature.goal, currency, locale)),
                    style = MaterialTheme.typography.bodySmall,
                )
            }
            if (backed) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.ok)
                    Text(stringResource(R.string.funding_backed), style = MaterialTheme.typography.labelLarge, color = colors.ok)
                }
            }
            if (feature.fundable) PriceButtons(priced(feature.products, prices), onBuy)
            feature.issue?.let { url ->
                TextButton(onClick = { onDiscuss(url) }) { Text(stringResource(R.string.funding_discuss)) }
            }
        }
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun PriceButtons(products: List<Pair<String, ProductPrice>>, onBuy: (String) -> Unit) {
    if (products.isEmpty()) return
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        products.forEach { (id, price) ->
            FilledTonalButton(onClick = { onBuy(id) }) { Text(price.formatted) }
        }
    }
}

@Composable
private fun PurchaseResultDialog(result: PurchaseResult, onDismiss: () -> Unit) {
    val (title, body) = when (result) {
        is PurchaseResult.Thanks -> stringResource(R.string.funding_thanks_title) to stringResource(R.string.funding_thanks_body)
        PurchaseResult.Pending -> stringResource(R.string.funding_title) to stringResource(R.string.funding_pending)
        is PurchaseResult.Failed -> stringResource(R.string.funding_title) to stringResource(R.string.funding_failed, result.message.asString())
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { Text(body) },
        confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) } },
    )
}

/** The [ids] the store sells, cheapest first. */
private fun priced(ids: List<String>, prices: Map<String, ProductPrice>): List<Pair<String, ProductPrice>> =
    ids.mapNotNull { id -> prices[id]?.let { id to it } }.sortedBy { it.second.micros }

/** [amount] whole units of [currency], e.g. "€200"; the bare number for an unknown currency. */
private fun money(amount: Int, currency: String, locale: Locale): String = runCatching {
    NumberFormat.getCurrencyInstance(locale).apply {
        this.currency = Currency.getInstance(currency)
        maximumFractionDigits = 0
    }.format(amount)
}.getOrElse { amount.toString() }
