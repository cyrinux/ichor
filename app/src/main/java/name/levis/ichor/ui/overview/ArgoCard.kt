package name.levis.ichor.ui.overview

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.allFine
import name.levis.ichor.model.levelCounts
import name.levis.ichor.model.likelyCause
import name.levis.ichor.model.problemApps
import name.levis.ichor.model.runningSyncs
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.apps.AppIconPlaceholder
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.argocd.ArgoAppIcon
import name.levis.ichor.ui.argocd.HealthGlyph
import name.levis.ichor.ui.argocd.causeText
import name.levis.ichor.ui.argocd.waveProgress
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.dataservices.color
import name.levis.ichor.ui.dataservices.label

/** Bar segments and counts, worst first. */
private val LEVELS = listOf(ServiceHealth.CRITICAL, ServiceHealth.WARNING, ServiceHealth.OK, ServiceHealth.IDLE, ServiceHealth.UNKNOWN)
private const val MAX_PROBLEMS = 2

/**
 * Argo CD at a glance, opening its screen: a segmented health bar and counts, the syncs running
 * with their wave progress, and up to two problem apps with their likely cause (a node Talos
 * reports down first). One calm line when every app is Synced and Healthy. Only composed when
 * the inventory shows Argo CD; a skeleton while loading, one muted line on failure.
 */
@Composable
fun ArgoCard(state: UiState<ArgoStatus>, argoTile: InventoryApp?, downNodes: Set<String>, onOpen: () -> Unit) {
    Card(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Header(argoTile, (state as? UiState.Loaded)?.data)
            when (state) {
                UiState.Loading -> Box(Modifier.fillMaxWidth().height(8.dp).clip(RoundedCornerShape(4.dp)).background(MaterialTheme.colorScheme.surfaceContainerHigh))
                is UiState.Failed -> MutedText(stringResource(R.string.data_services_unreadable, state.message.asString()), maxLines = 2, overflow = TextOverflow.Ellipsis)
                is UiState.Loaded -> Body(state.data, downNodes)
            }
        }
    }
}

@Composable
private fun Header(argoTile: InventoryApp?, status: ArgoStatus?) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        if (argoTile != null) AppIconTile(argoTile, size = 28.dp) else AppIconPlaceholder(size = 28.dp)
        Spacer(Modifier.size(12.dp))
        Text(stringResource(R.string.argo_card_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
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
private fun Body(status: ArgoStatus, downNodes: Set<String>) {
    if (status.apps.isEmpty()) {
        MutedText(stringResource(R.string.argo_no_apps), maxLines = 2, overflow = TextOverflow.Ellipsis)
        return
    }
    ArgoSummary(status)
    if (status.allFine) {
        MutedText(stringResource(R.string.argo_card_all_fine), maxLines = 2, overflow = TextOverflow.Ellipsis)
        return
    }
    status.runningSyncs.forEach { SyncingLine(it) }
    status.problemApps.take(MAX_PROBLEMS).forEach { ProblemLine(it, downNodes) }
}

/** The segmented health bar and "● 21 healthy ● 2 needs attention…": the overview card and the Argo CD app sheet. */
@Composable
fun ArgoSummary(status: ArgoStatus) {
    val counts = status.apps.levelCounts()
    SegmentedSummary(LEVELS.map { SummarySegment(counts[it] ?: 0, it.color(), it.label()) })
}

/** One part of a [SegmentedSummary]: how many, in which colour, called what. */
data class SummarySegment(val count: Int, val color: Color, val label: String)

/** A segmented bar sized by count over "● 21 healthy ● 2 needs attention…"; empty segments are left out. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SegmentedSummary(segments: List<SummarySegment>) {
    val shown = segments.filter { it.count > 0 }
    val total = shown.sumOf { it.count }.coerceAtLeast(1)
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(Modifier.fillMaxWidth().height(8.dp).clip(RoundedCornerShape(4.dp)), horizontalArrangement = Arrangement.spacedBy(2.dp)) {
            shown.forEach { Box(Modifier.weight(it.count.toFloat() / total).height(8.dp).background(it.color)) }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            shown.forEach {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.size(8.dp).clip(RoundedCornerShape(4.dp)).background(it.color))
                    Text("${it.count} ${it.label}", style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(start = 4.dp))
                }
            }
        }
    }
}

/** "cilium · wave 1 · 5/9" over a thin progress bar. */
@Composable
fun SyncingLine(app: ArgoApp) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        ArgoAppIcon(app, 24.dp)
        Column(Modifier.weight(1f).padding(start = 10.dp)) {
            Row {
                Text(app.name, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                Text(
                    " · " + stringResource(R.string.argo_syncing) + " · " + waveProgress(app),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.primary,
                    maxLines = 1,
                )
            }
            LinearProgressIndicator(progress = { app.operation?.progress ?: 0f }, strokeCap = StrokeCap.Round, modifier = Modifier.fillMaxWidth().padding(top = 4.dp))
        }
    }
}

/** "grafana · Degraded" and its likely cause underneath. */
@Composable
private fun ProblemLine(app: ArgoApp, downNodes: Set<String>) {
    val cause = app.likelyCause(downNodes)
    Row(verticalAlignment = Alignment.CenterVertically) {
        ArgoAppIcon(app, 24.dp)
        Column(Modifier.weight(1f).padding(start = 10.dp)) {
            Text(
                "${app.name} · ${listOf(app.health, app.sync).filter { it.isNotEmpty() && it != "Healthy" && it != "Synced" }.joinToString(" · ").ifEmpty { app.health }}",
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (cause != null) {
                Text(causeText(cause), style = MaterialTheme.typography.bodySmall, color = app.serviceHealth.color(), maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        HealthGlyph(app.healthState)
    }
}
