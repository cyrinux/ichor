package name.levis.ichor.ui.argocd

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.likelyCause
import name.levis.ichor.ui.components.QuickActionRow
import name.levis.ichor.ui.components.RowAction
import name.levis.ichor.ui.components.color
import name.levis.ichor.util.timeAgo

private val ICON = 40.dp

/**
 * An app row that swipes: right to sync, left to refresh; a long press lists Sync, Refresh and
 * Select. The row snaps back and the action runs; swiping is off while [swipeEnabled] is false
 * (multi-select, a request in flight, or [denial]: why the account cannot sync here).
 */
@Composable
fun SwipeableArgoRow(
    swipeEnabled: Boolean,
    denial: String?,
    onSync: () -> Unit,
    onRefresh: () -> Unit,
    onSelect: () -> Unit,
    onClick: () -> Unit,
    onClickLabel: String?,
    content: @Composable () -> Unit,
) {
    val sync = RowAction(Icons.Outlined.Sync, stringResource(R.string.argo_sync), onSync, enabled = swipeEnabled, disabledReason = denial)
    val refresh = RowAction(Icons.Outlined.Refresh, stringResource(R.string.argo_refresh), onRefresh, enabled = swipeEnabled, disabledReason = denial)
    val select = RowAction(Icons.Outlined.CheckCircle, stringResource(R.string.argo_select), onSelect)
    QuickActionRow(
        leading = sync,
        trailing = refresh,
        menu = listOf(sync, refresh, select),
        onClick = onClick,
        swipeEnabled = swipeEnabled,
        onClickLabel = onClickLabel,
        content = content,
    )
}

/**
 * Icon, name with its owner, chart@version or revision and when it was deployed, its health
 * and sync glyphs; under it the running sync's wave progress, or the likely cause of a problem.
 */
@Composable
fun ArgoAppRow(
    app: ArgoApp,
    downNodes: Set<String>,
    selected: Boolean,
    busy: Boolean,
) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val background = if (selected) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface
    // Tap, long press and the TalkBack actions belong to the SwipeableArgoRow around it.
    Row(
        Modifier.fillMaxWidth().background(background)
            .semantics { this.selected = selected }
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (selected) {
            Box(Modifier.size(ICON).clip(CircleShape).background(MaterialTheme.colorScheme.primary), contentAlignment = Alignment.Center) {
                Icon(Icons.Filled.CheckCircle, contentDescription = null, tint = MaterialTheme.colorScheme.onPrimary)
            }
        } else {
            ArgoAppIcon(app, ICON)
        }
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    app.name,
                    style = MaterialTheme.typography.titleSmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false),
                )
                app.owner?.let { OwnerChip(it, Modifier.padding(start = 6.dp)) }
                if (app.freeze != null) {
                    Icon(
                        Icons.Outlined.AcUnit,
                        contentDescription = stringResource(R.string.argo_badge_frozen),
                        tint = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.padding(start = 6.dp).size(16.dp),
                    )
                }
            }
            Text(
                listOf(app.versionLabel, timeAgo(app.deployedAt)).filter { it.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            RowDetail(app, downNodes)
        }
        Spacer(Modifier.size(8.dp))
        if (busy) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        } else {
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                HealthGlyph(app.healthState)
                SyncGlyph(app.syncState)
            }
        }
    }
}

@Composable
private fun RowDetail(app: ArgoApp, downNodes: Set<String>) {
    val op = app.operation
    if (op != null && op.isRunning) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 2.dp)) {
            LinearProgressIndicator(
                progress = { op.progress },
                strokeCap = StrokeCap.Round,
                modifier = Modifier.weight(1f).padding(end = 8.dp),
            )
            Text(waveProgress(app), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        }
        return
    }
    if (!app.serviceHealth.needsAttention) return
    val cause = app.likelyCause(downNodes)
    val text = cause?.let { causeText(it) } ?: return
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        color = app.serviceHealth.color(),
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
    )
}
