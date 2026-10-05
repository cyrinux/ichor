package name.levis.ichor.ui.flux

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FluxCondition
import name.levis.ichor.model.FluxHistory
import name.levis.ichor.model.FluxResource
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * One row per condition: type and status, reason, message and when it last changed. A false
 * Ready or Healthy, or a true Stalled, reads red; Reconciling blue; the rest muted or green.
 */
@Composable
fun FluxConditions(conditions: List<FluxCondition>) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        conditions.forEach { c ->
            val color = c.color()
            Surface(color = color.copy(alpha = 0.10f), shape = RoundedCornerShape(12.dp), modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.size(8.dp).background(color, CircleShape))
                        Text("${c.type}: ${c.status}", style = MaterialTheme.typography.labelLarge, color = color, modifier = Modifier.padding(start = 8.dp).weight(1f))
                        Text(timeAgo(c.at), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    if (c.reason.isNotEmpty()) Text(c.reason, style = MaterialTheme.typography.labelMedium)
                    if (c.message.isNotEmpty()) Text(c.message, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

@Composable
private fun FluxCondition.color(): Color {
    val colors = LocalStatusColors.current
    return when {
        type == "Stalled" && status == "True" -> colors.bad
        type == "Reconciling" && status == "True" -> MaterialTheme.colorScheme.primary
        status == "False" && type != "Stalled" && type != "Reconciling" -> colors.bad
        status == "True" -> colors.ok
        else -> colors.muted
    }
}

/** A kind of a Kustomization's inventory: its title and objects; workloads offer a rollout restart ([onRestart]). */
@Composable
fun FluxResourceGroup(kind: String, resources: List<FluxResource>, onRestart: ((FluxResource) -> Unit)?) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 4.dp)) {
            Text(kind, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
            Text(resources.size.toString(), style = MaterialTheme.typography.labelMedium, color = muted)
        }
        resources.forEach { r ->
            Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f).padding(start = 8.dp)) {
                    Text(r.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    if (r.namespace.isNotEmpty()) Text(r.namespace, style = MaterialTheme.typography.labelSmall, color = muted)
                }
                if (onRestart != null && r.restartable) {
                    IconButton(onClick = { onRestart(r) }) {
                        Icon(Icons.Outlined.RestartAlt, contentDescription = stringResource(R.string.workloads_restart_confirm), tint = muted, modifier = Modifier.size(18.dp))
                    }
                }
            }
        }
    }
}

/**
 * One Helm release of a HelmRelease's history, as a timeline entry: its revision, chart and app
 * versions, its status (failed in red) and when it was deployed. The newest is what runs.
 */
@Composable
fun FluxHistoryRow(entry: FluxHistory, current: Boolean, last: Boolean) {
    val colors = LocalStatusColors.current
    val accent = when {
        entry.failed -> colors.bad
        current -> MaterialTheme.colorScheme.primary
        else -> MaterialTheme.colorScheme.outline
    }
    Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min)) {
        Column(Modifier.width(26.dp).fillMaxHeight(), horizontalAlignment = Alignment.CenterHorizontally) {
            Box(Modifier.padding(top = 6.dp).size(10.dp).background(accent, CircleShape))
            if (!last) Box(Modifier.padding(top = 2.dp).width(2.dp).weight(1f).background(MaterialTheme.colorScheme.outlineVariant))
        }
        Column(Modifier.weight(1f).padding(start = 8.dp, bottom = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    stringResource(R.string.flux_release_revision, entry.version, entry.chartVersion),
                    style = MaterialTheme.typography.bodyMedium,
                    fontFamily = FontFamily.Monospace,
                    color = if (current || entry.failed) accent else MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                Text(entry.status, style = MaterialTheme.typography.labelSmall, color = accent)
            }
            Text(
                listOf(
                    entry.appVersion.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.flux_app_version, it) }.orEmpty(),
                    timeAgo(entry.deployedAt),
                ).filter { it.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
