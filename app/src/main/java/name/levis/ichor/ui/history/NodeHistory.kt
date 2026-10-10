package name.levis.ichor.ui.history

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import java.util.Locale

/**
 * The node's uptime strip, from the background checks' history (7 or 30 days); nothing until
 * the history has seen the node.
 */
@Composable
fun NodeUptime(node: String, modifier: Modifier = Modifier) {
    var days by rememberSaveable { mutableIntStateOf(HISTORY_SHORT_DAYS) }
    val history = rememberClusterHistory(days) ?: return
    val span = history.nodes.find { it.node == node } ?: return
    Column(modifier) {
        MutedText(stringResource(R.string.history_uptime_title))
        UptimeRow(span, history, days, onDays = { days = it })
    }
}

/** The node's memory use over the last 7 days, as a small line under its memory bar; nothing without points. */
@Composable
fun NodeMemoryHistory(node: String, modifier: Modifier = Modifier) {
    val history = rememberClusterHistory(HISTORY_SHORT_DAYS) ?: return
    val series = history.memory.find { it.node == node }?.series?.takeIf { it.size >= 2 } ?: return
    val description = pluralStringResource(R.plurals.history_memory_description, HISTORY_SHORT_DAYS, HISTORY_SHORT_DAYS)
    Column(modifier) {
        MutedText(description)
        PercentSparkline(series, history.from, history.to, description)
    }
}

/** How full the node's volumes were over the last 7 days, one line each; nothing without points. */
@Composable
fun VolumeFillHistory(node: String) {
    val history = rememberClusterHistory(HISTORY_SHORT_DAYS) ?: return
    val volumes = history.volumes.filter { it.node == node && it.series.size >= 2 }
    if (volumes.isEmpty()) return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            SectionTitle(pluralStringResource(R.plurals.history_volumes_title, HISTORY_SHORT_DAYS, HISTORY_SHORT_DAYS))
            volumes.forEach { volume ->
                val name = volume.name.ifBlank { volume.key.substringAfter('|') }
                val last = volume.series.last().getOrNull(1)
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(name, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(0.4f))
                    PercentSparkline(
                        volume.series,
                        history.from,
                        history.to,
                        stringResource(R.string.history_volume_description, name),
                        Modifier.weight(0.45f),
                    )
                    Text(last?.let { String.format(Locale.ROOT, "%.0f %%", it) } ?: "—", style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}
