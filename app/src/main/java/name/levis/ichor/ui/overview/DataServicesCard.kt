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
import name.levis.ichor.model.AllServiceFamilies
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.PluginDetectItem
import name.levis.ichor.model.line
import name.levis.ichor.ui.plugins.PluginIcon
import name.levis.ichor.model.ServiceFamily
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.family
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
    /** The active CRD plugins of Data services, each a line after (or among) the native systems. */
    plugins: List<PluginDetectItem> = emptyList(),
    onPlugin: (PluginDetectItem) -> Unit = {},
    /**
     * The families listed: storage alone on the Kubernetes home, whose tools card files the others;
     * every one on a Talos cluster, which has no tools card yet.
     */
    families: Set<ServiceFamily> = AllServiceFamilies,
) {
    when (state) {
        UiState.Loading -> Frame(count = null, levels = emptyList(), { onOpen(null) }) {
            hinted.filter { it.family in families }.forEach { kind -> Line(kind, apps, text = null, health = null) }
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
            val lines = remember(services, plugins, families) {
                (services.detected(families).map { CardLine.Native(it, services.summary(it)?.health) } + plugins.map { CardLine.Plugin(it) })
                    .sortedBy { (it.health ?: ServiceHealth.UNKNOWN).ordinal }
            }
            if (lines.isEmpty()) return
            Frame(lines.size, lines.mapNotNull { it.health }, { onOpen(null) }) {
                lines.forEach { line ->
                    when (line) {
                        is CardLine.Native -> Line(line.kind, apps, summaryText(line.kind, services), line.health, onClick = { onOpen(line.kind) })
                        is CardLine.Plugin -> PluginLine(line.item, onClick = { onPlugin(line.item) })
                    }
                }
            }
        }
    }
}

/** A line of the card: a native system, or a CRD plugin with its summary tile. */
private sealed interface CardLine {
    val health: ServiceHealth?

    data class Native(val kind: DataServiceKind, override val health: ServiceHealth?) : CardLine

    data class Plugin(val item: PluginDetectItem) : CardLine {
        override val health: ServiceHealth? get() = item.summary?.serviceHealth
    }
}

/** A plugin: its icon, name and summary ("2 sleep schedules · 1 asleep"); [onClick] opens its tab or screen. */
@Composable
private fun PluginLine(item: PluginDetectItem, onClick: () -> Unit) {
    Row(Modifier.clip(MaterialTheme.shapes.small).clickable(onClick = onClick).fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        PluginIcon(item.icon, item.name, ICON)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f)) {
            Text(item.name, style = MaterialTheme.typography.bodyMedium)
            item.summary?.line()?.takeIf { it.isNotEmpty() }?.let { MutedText(it, maxLines = 2, overflow = TextOverflow.Ellipsis) }
        }
        item.summary?.let { HealthDot(it.serviceHealth) }
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
