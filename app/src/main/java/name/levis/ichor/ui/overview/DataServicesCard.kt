package name.levis.ichor.ui.overview

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.detected
import name.levis.ichor.model.summary
import name.levis.ichor.model.title
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.apps.AppIconPlaceholder
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.dataservices.KindIcon
import name.levis.ichor.ui.dataservices.summaryText

private val ICON = 28.dp

/**
 * Longhorn, Garage and CloudNativePG at a glance, opening the Data services screen: one line
 * per system, the worst first, under one "N need a look" pill. The node that explains the
 * problems is named on the Data services screen, not here: the summary and Nodes cards already
 * say it is down. Only composed when the inventory shows one of them; a skeleton while loading,
 * one muted line on failure so the overview stays calm. Tapping a system's line opens its tab.
 */
@Composable
fun DataServicesCard(
    state: UiState<DataServices>,
    hinted: List<DataServiceKind>,
    apps: Map<String, InventoryApp>,
    onOpen: (DataServiceKind?) -> Unit,
) {
    when (state) {
        UiState.Loading -> Frame(count = null, levels = emptyList(), { onOpen(null) }) {
            hinted.forEach { kind -> Line(kind, apps, text = null, health = null) }
        }
        is UiState.Failed -> Frame(count = null, levels = emptyList(), { onOpen(null) }) {
            MutedText(
                stringResource(R.string.data_services_unreadable, state.message.asString()),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        is UiState.Loaded -> {
            val services = state.data
            // Worst first: ServiceHealth lists CRITICAL, then WARNING, before the calm levels.
            val kinds = remember(services) {
                services.detected.sortedBy { (services.summary(it)?.health ?: ServiceHealth.UNKNOWN).ordinal }
            }
            if (kinds.isEmpty()) return
            val levels = kinds.mapNotNull { services.summary(it)?.health }
            Frame(kinds.size, levels, { onOpen(null) }) {
                kinds.forEach { kind ->
                    Line(kind, apps, summaryText(kind, services), services.summary(kind)?.health, onClick = { onOpen(kind) })
                }
            }
        }
    }
}

@Composable
private fun Frame(count: Int?, levels: List<ServiceHealth>, onOpen: () -> Unit, content: @Composable () -> Unit) {
    SectionCard(
        stringResource(R.string.data_services_title),
        subtitle = count?.let { pluralStringResource(R.plurals.overview_data_services_count, it, it) },
        pill = attentionPill(levels),
        onOpen = onOpen,
    ) { content() }
}

/** A system: its icon, name and one line; a placeholder while [text] is null. [onClick] opens its tab. */
@Composable
private fun Line(
    kind: DataServiceKind,
    apps: Map<String, InventoryApp>,
    text: String?,
    health: ServiceHealth?,
    onClick: (() -> Unit)? = null,
) {
    val clickable = if (onClick != null) Modifier.clip(MaterialTheme.shapes.small).clickable(onClick = onClick) else Modifier
    Row(clickable.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        if (text == null) AppIconPlaceholder(size = ICON) else KindIcon(kind, apps[kind.catalogId], ICON)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f)) {
            Text(kind.title, style = MaterialTheme.typography.bodyMedium)
            if (text != null) {
                MutedText(text, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        if (health != null) HealthDot(health)
    }
}
