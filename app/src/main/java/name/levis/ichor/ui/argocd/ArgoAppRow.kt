package name.levis.ichor.ui.argocd

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.likelyCause
import name.levis.ichor.ui.dataservices.color
import name.levis.ichor.util.timeAgo

private val ICON = 40.dp

/**
 * An app row that swipes: right to sync, left to refresh. The row snaps back and the action
 * runs; swiping is off while [swipeEnabled] is false (multi-select, a request in flight).
 */
@Composable
fun SwipeableArgoRow(swipeEnabled: Boolean, onSync: () -> Unit, onRefresh: () -> Unit, content: @Composable () -> Unit) {
    val state = rememberSwipeToDismissBoxState()
    LaunchedEffect(state.currentValue) {
        when (state.currentValue) {
            SwipeToDismissBoxValue.StartToEnd -> onSync()
            SwipeToDismissBoxValue.EndToStart -> onRefresh()
            SwipeToDismissBoxValue.Settled -> return@LaunchedEffect
        }
        state.reset()
    }
    SwipeToDismissBox(
        state = state,
        enableDismissFromStartToEnd = swipeEnabled,
        enableDismissFromEndToStart = swipeEnabled,
        backgroundContent = { SwipeBackground(state.dismissDirection) },
    ) { content() }
}

@Composable
private fun SwipeBackground(direction: SwipeToDismissBoxValue) {
    if (direction == SwipeToDismissBoxValue.Settled) return
    val sync = direction == SwipeToDismissBoxValue.StartToEnd
    val container = if (sync) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.tertiaryContainer
    val content = if (sync) MaterialTheme.colorScheme.onPrimaryContainer else MaterialTheme.colorScheme.onTertiaryContainer
    Box(
        Modifier.fillMaxSize().background(container).padding(horizontal = 24.dp),
        contentAlignment = if (sync) Alignment.CenterStart else Alignment.CenterEnd,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(if (sync) Icons.Outlined.Sync else Icons.Outlined.Refresh, contentDescription = null, tint = content)
            Text(stringResource(if (sync) R.string.argo_sync else R.string.argo_refresh), color = content, style = MaterialTheme.typography.labelLarge)
        }
    }
}

/**
 * Icon, name with its owner, chart@version or revision and when it was deployed, its health
 * and sync glyphs; under it the running sync's wave progress, or the likely cause of a problem.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ArgoAppRow(
    app: ArgoApp,
    downNodes: Set<String>,
    selected: Boolean,
    busy: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    onClickLabel: String? = null,
    onSync: (() -> Unit)? = null,
    onRefresh: (() -> Unit)? = null,
) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val background = if (selected) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface
    // The swipes, for TalkBack and switch access: offered only while the row can swipe.
    val syncLabel = stringResource(R.string.argo_sync)
    val refreshLabel = stringResource(R.string.argo_refresh)
    Row(
        Modifier.fillMaxWidth().background(background)
            .combinedClickable(
                onClick = onClick,
                onClickLabel = onClickLabel,
                onLongClick = onLongClick,
                onLongClickLabel = stringResource(R.string.argo_select),
            )
            .semantics {
                this.selected = selected
                customActions = listOfNotNull(
                    onSync?.let { CustomAccessibilityAction(syncLabel) { it(); true } },
                    onRefresh?.let { CustomAccessibilityAction(refreshLabel) { it(); true } },
                )
            }
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
