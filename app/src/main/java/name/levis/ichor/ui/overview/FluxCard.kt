package name.levis.ichor.ui.overview

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.FluxState
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.allFine
import name.levis.ichor.model.failingApps
import name.levis.ichor.model.stateCounts
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.apps.AppIconPlaceholder
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.flux.FluxAppIcon
import name.levis.ichor.ui.flux.FluxStateGlyph
import name.levis.ichor.ui.flux.color
import name.levis.ichor.ui.flux.label

/** Bar segments and counts, worst first. */
private val STATES = listOf(FluxState.FAILING, FluxState.RECONCILING, FluxState.READY, FluxState.SUSPENDED)
private const val MAX_FAILING = 2

/**
 * Flux at a glance, opening its screen: a segmented bar with how many Kustomizations and
 * HelmReleases are ready, reconciling, failing or suspended, and up to two failing ones with
 * their reason. One calm line when nothing fails or reconciles. Only composed when the inventory
 * shows Flux; a skeleton while loading, one muted line on failure.
 */
@Composable
fun FluxCard(state: UiState<FluxStatus>, fluxTile: InventoryApp?, onOpen: () -> Unit) {
    Card(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Header(fluxTile, (state as? UiState.Loaded)?.data)
            when (state) {
                UiState.Loading -> Box(Modifier.fillMaxWidth().height(8.dp).clip(RoundedCornerShape(4.dp)).background(MaterialTheme.colorScheme.surfaceContainerHigh))
                is UiState.Failed -> MutedText(stringResource(R.string.data_services_unreadable, state.message.asString()), maxLines = 2, overflow = TextOverflow.Ellipsis)
                is UiState.Loaded -> Body(state.data)
            }
        }
    }
}

@Composable
private fun Header(fluxTile: InventoryApp?, status: FluxStatus?) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        if (fluxTile != null) AppIconTile(fluxTile, size = 28.dp) else AppIconPlaceholder(size = 28.dp)
        Spacer(Modifier.size(12.dp))
        Text(stringResource(R.string.flux_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
        if (status != null) {
            Text(
                pluralStringResource(R.plurals.argo_apps, status.apps.size, status.apps.size),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Icon(
            Icons.AutoMirrored.Outlined.KeyboardArrowRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 4.dp),
        )
    }
}

@Composable
private fun Body(status: FluxStatus) {
    if (status.apps.isEmpty()) {
        MutedText(stringResource(if (status.installed) R.string.flux_no_apps else R.string.flux_not_installed), maxLines = 2, overflow = TextOverflow.Ellipsis)
        return
    }
    FluxSummary(status)
    if (status.allFine) {
        MutedText(stringResource(R.string.flux_card_all_fine), maxLines = 2, overflow = TextOverflow.Ellipsis)
        return
    }
    status.failingApps.take(MAX_FAILING).forEach { FailingLine(it) }
}

/** The segmented bar and "● 5 ready ● 2 failing…": the overview card and the Flux tile's sheet. */
@Composable
fun FluxSummary(status: FluxStatus) {
    val counts = status.apps.stateCounts()
    SegmentedSummary(STATES.map { SummarySegment(counts[it] ?: 0, it.color(), it.label()) })
}

/** "ingress-nginx · HelmRelease" and its reason and message underneath. */
@Composable
private fun FailingLine(app: FluxApp) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        FluxAppIcon(app, 24.dp)
        Column(Modifier.weight(1f).padding(start = 10.dp)) {
            Text("${app.name} · ${app.kind}", style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val why = listOf(app.reason, app.message).filter { it.isNotEmpty() }.joinToString(": ")
            if (why.isNotEmpty()) {
                Text(why, style = MaterialTheme.typography.bodySmall, color = app.state.color(), maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        FluxStateGlyph(app.state)
    }
}
