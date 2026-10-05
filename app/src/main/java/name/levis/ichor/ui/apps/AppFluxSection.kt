package name.levis.ichor.ui.apps

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.allFine
import name.levis.ichor.model.failingApps
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.overview.FluxSummary

/** What the Flux tile's own sheet needs: Flux's state and the way to its screen; null on any other tile. */
data class AppFluxUi(val state: UiState<FluxStatus>, val onOpenFlux: () -> Unit)

/** The Flux tile: the GitOps summary (ready, reconciling, failing, suspended) and a button opening the Flux screen. */
fun LazyListScope.appFluxSection(flux: AppFluxUi) {
    item(key = "flux-title") { SectionTitle(stringResource(R.string.flux_title)) }
    item(key = "flux-summary") { FluxSummaryBlock(flux.state, flux.onOpenFlux) }
}

@Composable
private fun FluxSummaryBlock(state: UiState<FluxStatus>, onOpen: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        when (state) {
            UiState.Loading -> MutedText(stringResource(R.string.flux_loading))
            is UiState.Failed -> MutedText(stringResource(R.string.data_services_unreadable, state.message.asString()))
            is UiState.Loaded -> {
                FluxSummary(state.data)
                val failing = state.data.failingApps
                when {
                    state.data.apps.isEmpty() -> MutedText(stringResource(R.string.flux_no_apps))
                    state.data.allFine -> MutedText(stringResource(R.string.flux_card_all_fine))
                    failing.isNotEmpty() -> MutedText(stringResource(R.string.flux_failing_names, failing.joinToString(", ") { it.name }))
                }
            }
        }
        FilledTonalButton(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.flux_open_screen))
        }
    }
}
