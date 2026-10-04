package name.levis.ichor.ui.apps

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.argocd.HealthGlyph
import name.levis.ichor.ui.argocd.OwnerChip
import name.levis.ichor.ui.argocd.SyncGlyph
import name.levis.ichor.ui.argocd.waveProgress
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.overview.ArgoSummary
import name.levis.ichor.ui.overview.SyncingLine
import name.levis.ichor.util.timeAgo

/** What the app sheet needs to show the app's Argo CD Applications; null where Argo CD is not offered. */
data class AppArgoUi(
    val state: UiState<ArgoStatus>,
    /** The Applications deploying the app (see [name.levis.ichor.model.argoAppsFor]). */
    val apps: List<ArgoApp>,
    /** The sheet is Argo CD's own: it shows the GitOps summary instead. */
    val isArgoCD: Boolean,
    /** Keys of the apps whose action is in flight. */
    val busy: Set<String>,
    val onSync: (ArgoApp) -> Unit,
    val onRefresh: (ArgoApp) -> Unit,
    val onOpen: (ArgoApp) -> Unit,
    val onOpenArgoCD: () -> Unit,
)

/** Argo CD's tile: the GitOps summary; any other app: one card per Application deploying it. */
fun LazyListScope.appArgoSection(argo: AppArgoUi) {
    val loaded = (argo.state as? UiState.Loaded)?.data
    if (argo.isArgoCD) {
        item(key = "argo-title") { SectionTitle(stringResource(R.string.argo_card_title)) }
        item(key = "argo-summary") { ArgoCDSummary(argo.state, argo.onOpenArgoCD) }
        return
    }
    // Nothing deploys it, or not known yet: the section stays out of the way.
    if (loaded == null || argo.apps.isEmpty()) {
        if (argo.state is UiState.Failed) item(key = "argo-failed") {
            SectionTitle(stringResource(R.string.argo_title))
            MutedText(stringResource(R.string.data_services_unreadable, argo.state.message.asString()))
        }
        return
    }
    item(key = "argo-title") { SectionTitle(stringResource(R.string.argo_title)) }
    items(argo.apps, key = { "argo/${it.key}" }) { app ->
        ArgoAppCard(app, app.key in argo.busy, { argo.onSync(app) }, { argo.onRefresh(app) }, { argo.onOpen(app) })
    }
}

@Composable
private fun ArgoCDSummary(state: UiState<ArgoStatus>, onOpen: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        when (state) {
            UiState.Loading -> MutedText(stringResource(R.string.argo_loading))
            is UiState.Failed -> MutedText(stringResource(R.string.data_services_unreadable, state.message.asString()))
            is UiState.Loaded -> {
                ArgoSummary(state.data)
                state.data.apps.filter { it.isRunning }.forEach { SyncingLine(it) }
            }
        }
        FilledTonalButton(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.argo_open_screen))
        }
    }
}

/**
 * Health and sync, version and when it was deployed, its owner, the running sync, Sync (after a
 * confirmation, never pruning) and Refresh, and a row opening the app's Argo CD screen.
 */
@Composable
private fun ArgoAppCard(app: ArgoApp, busy: Boolean, onSync: () -> Unit, onRefresh: () -> Unit, onOpen: () -> Unit) {
    var confirm by remember { mutableStateOf(false) }
    if (confirm) {
        ConfirmDialog(
            title = stringResource(R.string.argo_sync_title, app.name),
            text = stringResource(R.string.argo_sync_quick_text, app.versionLabel),
            confirm = stringResource(R.string.argo_sync),
            onConfirm = { confirm = false; onSync() },
            onDismiss = { confirm = false },
        )
    }
    Surface(shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = stringResource(R.string.common_open), onClick = onOpen), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(app.name, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                        HealthGlyph(app.healthState, 16.dp)
                        Text(app.health, style = MaterialTheme.typography.labelMedium)
                        SyncGlyph(app.syncState, 16.dp)
                        Text(app.sync, style = MaterialTheme.typography.labelMedium)
                    }
                    Text(
                        listOf(app.versionLabel, timeAgo(app.deployedAt)).filter { it.isNotEmpty() }.joinToString(" · "),
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    app.owner?.let { OwnerChip(it, Modifier.padding(top = 4.dp)) }
                }
                Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = stringResource(R.string.argo_open_app))
            }
            if (app.isRunning) {
                Text(
                    stringResource(R.string.argo_syncing) + " · " + waveProgress(app),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                FilledTonalButton(onClick = { confirm = true }, enabled = !busy && !app.isRunning) {
                    Icon(Icons.Outlined.Sync, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.argo_sync), modifier = Modifier.padding(start = 6.dp))
                }
                OutlinedButton(onClick = onRefresh, enabled = !busy) {
                    Icon(Icons.Outlined.Refresh, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.argo_refresh), modifier = Modifier.padding(start = 6.dp))
                }
                if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            }
        }
    }
}
