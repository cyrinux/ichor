package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoCondition
import name.levis.ichor.model.ArgoSource
import name.levis.ichor.model.shortRevision
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The app at a glance: icon and name, big health and sync badges, where it comes from and goes
 * to, its links, and the auto-sync switch, disabled with the reason on an owned app.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ArgoAppHero(app: ArgoApp, autoSyncBusy: Boolean, onAutoSync: (Boolean) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            ArgoAppIcon(app, 64.dp)
            Column(Modifier.padding(start = 16.dp).weight(1f)) {
                Text(app.name, style = MaterialTheme.typography.headlineSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                MutedText(listOf(app.project, app.namespace).filter { it.isNotEmpty() }.joinToString(" · "))
                app.owner?.let { OwnerChip(it, Modifier.padding(top = 4.dp)) }
            }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            ArgoBadge(app.healthState.icon, app.healthState.wire, app.healthState.color())
            ArgoBadge(app.syncState.icon, app.syncState.wire, app.syncState.color())
            if (app.refreshing.isNotEmpty()) ArgoBadge(Icons.Outlined.Refresh, stringResource(R.string.argo_refreshing), MaterialTheme.colorScheme.primary)
        }
        if (app.healthMessage.isNotEmpty()) {
            Text(app.healthMessage, style = MaterialTheme.typography.bodySmall, color = app.healthState.color())
        }
        Column {
            app.destination.namespace.takeIf { it.isNotEmpty() }?.let { InfoRow(stringResource(R.string.argo_destination), it, mono = true) }
            app.sources.forEach { SourceRows(it) }
            if (app.revision.isNotEmpty()) InfoRow(stringResource(R.string.argo_revision), shortRevision(app.revision), mono = true)
        }
        if (app.externalURLs.isNotEmpty()) Links(app.externalURLs)
        AutoSyncRow(app, autoSyncBusy, onAutoSync)
    }
}

@Composable
private fun SourceRows(src: ArgoSource) {
    InfoRow(stringResource(R.string.argo_repo), src.repo, mono = true)
    when {
        src.chart.isNotEmpty() -> InfoRow(stringResource(R.string.argo_chart), "${src.chart}@${src.targetRevision}", mono = true)
        src.path.isNotEmpty() -> InfoRow(stringResource(R.string.argo_path), "${src.path} @ ${src.targetRevision}", mono = true)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Links(urls: List<String>) {
    val uri = LocalUriHandler.current
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        urls.forEach { url ->
            AssistChip(
                onClick = { runCatching { uri.openUri(url) } },
                label = { Text(url.removePrefix("https://").removePrefix("http://").trimEnd('/'), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingIcon = { Icon(Icons.AutoMirrored.Outlined.OpenInNew, contentDescription = stringResource(R.string.argo_open_link), modifier = Modifier.size(18.dp)) },
            )
        }
    }
}

@Composable
private fun AutoSyncRow(app: ArgoApp, busy: Boolean, onAutoSync: (Boolean) -> Unit) {
    Surface(shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.surfaceContainer) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.Bolt, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Column(Modifier.weight(1f).padding(horizontal = 12.dp)) {
                Text(stringResource(R.string.argo_auto_sync), style = MaterialTheme.typography.bodyLarge)
                val detail = when {
                    !app.canChangeSpec -> stringResource(R.string.argo_owned, app.owner?.kind.orEmpty(), app.owner?.name.orEmpty())
                    app.autoSync.enabled -> listOfNotNull(
                        stringResource(R.string.argo_auto_prune).takeIf { app.autoSync.prune },
                        stringResource(R.string.argo_auto_self_heal).takeIf { app.autoSync.selfHeal },
                    ).joinToString(" · ")
                    else -> stringResource(R.string.argo_auto_sync_paused)
                }
                if (detail.isNotEmpty()) MutedText(detail)
            }
            Switch(checked = app.autoSync.enabled, onCheckedChange = onAutoSync, enabled = app.canChangeSpec && !busy)
        }
    }
}

/** One warning banner per condition: SyncError, ComparisonError, OrphanedResourceWarning... */
@Composable
fun ConditionBanners(conditions: List<ArgoCondition>) {
    val colors = LocalStatusColors.current
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        conditions.forEach { c ->
            val color = if (c.type.endsWith("Error")) colors.bad else colors.warn
            Surface(color = color.copy(alpha = 0.12f), contentColor = color, shape = RoundedCornerShape(12.dp), modifier = Modifier.fillMaxWidth()) {
                Row(Modifier.padding(12.dp), verticalAlignment = Alignment.Top) {
                    Icon(Icons.Outlined.WarningAmber, contentDescription = null, modifier = Modifier.size(20.dp))
                    Spacer(Modifier.size(8.dp))
                    Column {
                        Text(c.type, style = MaterialTheme.typography.labelLarge)
                        Text(c.message, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}

/** Sync (opens the sheet), Refresh and Hard refresh. */
@Composable
fun ArgoActionButtons(busy: Boolean, running: Boolean, onSync: () -> Unit, onRefresh: () -> Unit, onHardRefresh: () -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
        Button(onClick = onSync, enabled = !busy && !running, modifier = Modifier.weight(1f)) {
            Icon(Icons.Outlined.Sync, contentDescription = null, modifier = Modifier.size(18.dp))
            Text(stringResource(R.string.argo_sync), modifier = Modifier.padding(start = 6.dp), maxLines = 1)
        }
        OutlinedButton(onClick = onRefresh, enabled = !busy, modifier = Modifier.weight(1f)) {
            Text(stringResource(R.string.argo_refresh), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        OutlinedButton(onClick = onHardRefresh, enabled = !busy, modifier = Modifier.weight(1f)) {
            Text(stringResource(R.string.argo_hard_refresh), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}
