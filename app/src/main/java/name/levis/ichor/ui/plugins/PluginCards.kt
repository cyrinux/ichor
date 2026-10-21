package name.levis.ichor.ui.plugins

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.PluginCondition
import name.levis.ichor.model.PluginItem
import name.levis.ichor.model.PluginRef
import name.levis.ichor.model.PluginRow
import name.levis.ichor.model.PluginSection
import name.levis.ichor.model.PluginSummary
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.color
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.kubebrowser.FieldChip
import name.levis.ichor.ui.kubebrowser.ToneLabel
import name.levis.ichor.ui.theme.LocalStatusColors

// The render tree's primitives (docs/plugins.md "Render tree"), drawn once for every plugin.
// Colours come from the status tokens only; labels and values arrive translated from Go.

/** A plugin tone's colour: the status tokens, the theme's primary for info, muted otherwise. */
@Composable
fun pluginToneColor(tone: String): Color {
    val colors = LocalStatusColors.current
    return when (tone) {
        "ok" -> colors.ok
        "warning" -> colors.warn
        "critical" -> colors.bad
        "info" -> MaterialTheme.colorScheme.primary
        else -> colors.muted
    }
}

/** A tone's colour for a value, or the default text colour when the value has no tone. */
@Composable
private fun valueColor(tone: String): Color = if (tone.isEmpty()) Color.Unspecified else pluginToneColor(tone)

/** The plugin's icon from the app catalog, a monogram of its name when there is none. */
@Composable
fun PluginIcon(icon: String, name: String, size: Dp, modifier: Modifier = Modifier) {
    AppIconTile(InventoryApp(id = icon.ifEmpty { name }, name = name, icon = icon), modifier, size = size)
}

/** Badges as tinted chips: "1 asleep", "Deployments". */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PluginBadgeRow(labels: List<Pair<String, String>>, modifier: Modifier = Modifier) {
    if (labels.isEmpty()) return
    FlowRow(modifier, horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        labels.forEach { (text, tone) -> ToneLabel(text, pluginToneColor(tone)) }
    }
}

/** The summary as the tab's header: its health, title and badges, and "partial" when a list was cut. */
@Composable
fun PluginSummaryHeader(summary: PluginSummary, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(summary.serviceHealth)
            Spacer(Modifier.size(10.dp))
            Text(summary.title, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
        }
        PluginBadgeRow(summary.badges.map { b -> listOf(b.value, b.label).filter { it.isNotEmpty() }.joinToString(" ") to b.tone })
        if (summary.partial) MutedText(stringResource(R.string.plugin_partial))
    }
}

/**
 * A row of a plugin card, inside a grouped card: its health dot, title, subtitle (left out
 * when [showSubtitle] is false: the card's title says it), status and trailing text; it opens
 * on its detail sections when it has any. [extra] adds a native control under them; [actions]
 * runs the buttons of its actions sections (null: they stay off).
 */
@Composable
fun PluginRowContent(
    row: PluginRow,
    open: Boolean,
    onToggle: () -> Unit,
    onObject: (PluginRef) -> Unit,
    showSubtitle: Boolean = true,
    extra: (@Composable () -> Unit)? = null,
    actions: PluginActionRunner? = null,
) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val expandable = row.detail.any { it !is PluginSection.Unknown } || extra != null
    Column(Modifier.fillMaxWidth().expandable(open, enabled = expandable, onToggle = onToggle).animateContentSize().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            // A row with neither status nor health (a data service's) has no dot, not a grey "unknown" one.
            if (row.status != null || row.health != null) {
                HealthDot(row.serviceHealth)
                Spacer(Modifier.size(12.dp))
            }
            Column(Modifier.weight(1f)) {
                Text(row.title, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
                val sub = listOfNotNull(row.subtitle.takeIf { showSubtitle && it.isNotEmpty() }, row.health?.message?.takeIf { it.isNotEmpty() })
                if (sub.isNotEmpty()) {
                    Text(
                        sub.joinToString(" · "),
                        style = MaterialTheme.typography.labelSmall,
                        color = row.serviceHealth.color().takeIf { row.serviceHealth.needsAttention } ?: muted,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(2.dp)) {
                row.status?.takeIf { it.text.isNotEmpty() }?.let { ToneLabel(it.text, pluginToneColor(it.tone)) }
                row.trailing?.takeIf { it.text.isNotEmpty() }?.let { Text(it.text, style = MaterialTheme.typography.labelSmall, color = muted, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            }
            if (expandable) {
                Spacer(Modifier.size(4.dp))
                Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = muted)
            }
        }
        if (open) {
            Column(Modifier.padding(start = 22.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                row.detail.forEach { PluginSectionView(it, onObject, row.ref, actions) }
                extra?.invoke()
            }
        }
    }
}

/** One detail section of the row on [ref]; an unknown type draws nothing. [actions]: see [PluginActionButtons]. */
@Composable
fun PluginSectionView(section: PluginSection, onObject: (PluginRef) -> Unit, ref: PluginRef? = null, actions: PluginActionRunner? = null) {
    when (section) {
        is PluginSection.KeyValues -> KeyValues(section.items)
        is PluginSection.Badges -> PluginBadgeRow(section.items.map { b -> listOf(b.value, b.label).filter { it.isNotEmpty() }.joinToString(" ") to b.tone })
        is PluginSection.Gauge -> Gauge(section)
        is PluginSection.Conditions -> Conditions(section.items)
        is PluginSection.Relations -> Relations(section.items, onObject)
        is PluginSection.Table -> Table(section)
        is PluginSection.Text -> Text(
            section.text,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = if (section.mono) FontFamily.Monospace else null,
            color = valueColor(section.tone),
        )
        is PluginSection.Actions -> PluginActionButtons(section.items, ref, actions)
        is PluginSection.Unknown -> Unit
    }
}

@Composable
private fun KeyValues(items: List<PluginItem>) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        items.forEach { InfoRow(it.label, it.value.ifEmpty { "–" }, mono = it.mono, valueColor = valueColor(it.tone)) }
    }
}

/** A bar with its label and text ("0 B / 512 MiB"); a bare text when there is no maximum. */
@Composable
private fun Gauge(g: PluginSection.Gauge) {
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(g.label, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
            if (g.text.isNotEmpty()) Text(g.text, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
        }
        g.fraction?.let { f ->
            LinearProgressIndicator(
                progress = { f },
                modifier = Modifier.fillMaxWidth(),
                color = pluginToneColor(g.tone.ifEmpty { "ok" }),
                trackColor = MaterialTheme.colorScheme.surfaceVariant,
            )
        }
    }
}

/** The object's conditions, read like the object summary's: type tinted, status and reason, age, message. */
@Composable
private fun Conditions(items: List<PluginCondition>) {
    val now = System.currentTimeMillis()
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        items.forEach { c ->
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    ToneLabel(c.type, pluginToneColor(c.tone))
                    Text(
                        listOf(c.status, c.reason).filter { it.isNotEmpty() }.joinToString("  ·  "),
                        style = MaterialTheme.typography.labelMedium,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    if (c.lastTransition > 0) MutedText(stringResource(R.string.kube_events_ago, ageSince(c.lastTransition, now)), maxLines = 1)
                }
                if (c.message.isNotEmpty()) Text(c.message, style = MaterialTheme.typography.bodySmall, maxLines = 6, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/** Links to related objects: each opens the object screen. */
@Composable
private fun Relations(items: List<PluginItem>, onObject: (PluginRef) -> Unit) {
    Column {
        items.forEach { item ->
            val ref = item.ref
            val click = if (ref != null) Modifier.clickable(role = Role.Button) { onObject(ref) } else Modifier
            Row(Modifier.fillMaxWidth().then(click).padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(item.label, style = MaterialTheme.typography.labelMedium)
                    val target = item.text.ifEmpty { ref?.let { r -> listOf(r.namespace, r.name).filter { it.isNotEmpty() }.joinToString("/") }.orEmpty() }
                    if (target.isNotEmpty()) {
                        Text(target, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
                if (ref != null) Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null)
            }
        }
    }
}

/** A table as the resource list draws its rows: one line of label/value chips per row. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Table(t: PluginSection.Table) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        t.rows.forEach { cells ->
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                cells.forEachIndexed { i, cell -> FieldChip(t.columns.getOrNull(i)?.label.orEmpty(), cell.ifEmpty { "–" }) }
            }
        }
    }
}
