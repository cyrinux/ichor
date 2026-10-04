package name.levis.ichor.ui.argocd

import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.ArrowCircleUp
import androidx.compose.material.icons.outlined.Autorenew
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.HeartBroken
import androidx.compose.material.icons.outlined.PauseCircle
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoCause
import name.levis.ichor.model.ArgoHealth
import name.levis.ichor.model.ArgoOwner
import name.levis.ichor.model.ArgoSync
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.theme.LocalStatusColors

/** Argo CD's own palette for its states: green healthy, blue progressing, red degraded. */
@Composable
fun ArgoHealth.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        ArgoHealth.HEALTHY -> colors.ok
        ArgoHealth.PROGRESSING -> MaterialTheme.colorScheme.primary
        ArgoHealth.DEGRADED -> colors.bad
        ArgoHealth.MISSING -> colors.warn
        ArgoHealth.SUSPENDED, ArgoHealth.UNKNOWN -> colors.muted
    }
}

/** Heart, spinner, broken heart: Argo CD's health glyphs. */
val ArgoHealth.icon: ImageVector
    get() = when (this) {
        ArgoHealth.HEALTHY -> Icons.Outlined.Favorite
        ArgoHealth.PROGRESSING -> Icons.Outlined.Autorenew
        ArgoHealth.DEGRADED -> Icons.Outlined.HeartBroken
        ArgoHealth.SUSPENDED -> Icons.Outlined.PauseCircle
        ArgoHealth.MISSING -> Icons.Outlined.ErrorOutline
        ArgoHealth.UNKNOWN -> Icons.AutoMirrored.Outlined.HelpOutline
    }

@Composable
fun ArgoSync.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        ArgoSync.SYNCED -> colors.ok
        ArgoSync.OUT_OF_SYNC -> colors.warn
        ArgoSync.UNKNOWN -> colors.muted
    }
}

val ArgoSync.icon: ImageVector
    get() = when (this) {
        ArgoSync.SYNCED -> Icons.Outlined.CheckCircle
        ArgoSync.OUT_OF_SYNC -> Icons.Outlined.ArrowCircleUp
        ArgoSync.UNKNOWN -> Icons.AutoMirrored.Outlined.HelpOutline
    }

/** The app's catalog icon, or a monogram in its own colour. */
@Composable
fun ArgoAppIcon(app: ArgoApp, size: Dp, modifier: Modifier = Modifier) {
    val tile = InventoryApp(id = app.icon.ifEmpty { app.name }, name = app.name, icon = app.icon, remoteIcon = app.remoteIcon)
    AppIconTile(tile, modifier, size = size)
}

/** A small coloured glyph with the state's Argo CD name as its description. */
@Composable
fun ArgoGlyph(icon: ImageVector, color: Color, description: String, size: Dp = 18.dp) {
    Icon(icon, contentDescription = description, tint = color, modifier = Modifier.size(size))
}

@Composable
fun HealthGlyph(health: ArgoHealth, size: Dp = 18.dp) = ArgoGlyph(health.icon, health.color(), health.wire, size)

@Composable
fun SyncGlyph(sync: ArgoSync, size: Dp = 18.dp) = ArgoGlyph(sync.icon, sync.color(), sync.wire, size)

/** A big tinted badge: glyph and Argo CD's word for the state. */
@Composable
fun ArgoBadge(icon: ImageVector, label: String, color: Color, modifier: Modifier = Modifier) {
    Surface(modifier, shape = RoundedCornerShape(12.dp), color = color.copy(alpha = 0.14f), contentColor = color) {
        Row(Modifier.padding(horizontal = 12.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(icon, contentDescription = null, modifier = Modifier.size(20.dp))
            Text(label, style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(start = 8.dp))
        }
    }
}

/** "infra": the ApplicationSet or parent app writing the app's spec. */
@Composable
fun OwnerChip(owner: ArgoOwner, modifier: Modifier = Modifier) {
    Surface(modifier, shape = RoundedCornerShape(50), color = MaterialTheme.colorScheme.secondaryContainer) {
        Row(Modifier.padding(horizontal = 8.dp, vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.AccountTree, contentDescription = null, modifier = Modifier.size(12.dp))
            Text(
                owner.name,
                style = MaterialTheme.typography.labelSmall,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 4.dp),
            )
        }
    }
}

/** A dot on an inventory tile: the app's Argo CD app is critical (red) or OutOfSync (amber). */
@Composable
fun ArgoTileBadge(critical: Boolean, modifier: Modifier = Modifier, size: Dp = 18.dp) {
    val colors = LocalStatusColors.current
    val color = if (critical) colors.bad else colors.warn
    Box(
        modifier.size(size).background(MaterialTheme.colorScheme.surface, CircleShape).padding(2.dp)
            .background(color, CircleShape),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            if (critical) Icons.Outlined.HeartBroken else Icons.Outlined.ArrowCircleUp,
            contentDescription = stringResource(if (critical) R.string.argo_badge_critical else R.string.argo_badge_out_of_sync),
            tint = MaterialTheme.colorScheme.surface,
            modifier = Modifier.size(size * 0.6f),
        )
    }
}

@Composable
fun causeText(cause: ArgoCause): String = when (cause) {
    is ArgoCause.NodeDown -> stringResource(R.string.argo_cause_node, cause.node)
    is ArgoCause.PodStatus -> stringResource(R.string.argo_cause_pod, cause.pod, cause.status)
    is ArgoCause.Message -> cause.text
}

/** "wave 1 · 5/9". */
@Composable
fun waveProgress(app: ArgoApp): String {
    val op = app.operation ?: return ""
    val count = stringResource(R.string.argo_progress_count, op.done, op.total)
    return if (op.waves.size > 1) stringResource(R.string.argo_wave, op.wave) + " · " + count else count
}

/**
 * While the screen is started, polls every 2 s when [ArgoViewModel.shouldPoll] says a sync is
 * running or an action was just requested; nothing otherwise.
 */
@Composable
fun ArgoPolling(vm: ArgoViewModel) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(vm, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                delay(POLL_MILLIS)
                if (vm.shouldPoll) vm.poll()
            }
        }
    }
}

private const val POLL_MILLIS = 2_000L

/** The outcome of each action as a snackbar message, through [show]. */
@Composable
fun ArgoActionMessages(results: Flow<ArgoActionResult>, show: suspend (String) -> Unit) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r ->
            val text = when {
                r.error != null && r.count == 1 -> context.getString(R.string.argo_action_failed, r.error.resolve(context))
                r.error != null -> context.getString(R.string.argo_action_failed_some, r.failed, r.count, r.error.resolve(context))
                r.count > 1 -> context.resources.getQuantityString(R.plurals.argo_action_done_many, r.count, context.getString(r.action.doneLabel), r.count)
                else -> context.getString(r.action.doneLabel)
            }
            show(text)
        }
    }
}

/** The outcome of each action as a toast, where there is no snackbar (the app sheet). */
@Composable
fun ArgoActionToasts(results: Flow<ArgoActionResult>) {
    val context = LocalContext.current
    ArgoActionMessages(results) { Toast.makeText(context, it, Toast.LENGTH_SHORT).show() }
}

/** "Sync requested", "Refresh requested"... */
private val ArgoAction.doneLabel: Int
    get() = when (this) {
        ArgoAction.REFRESH -> R.string.argo_done_refresh
        ArgoAction.HARD_REFRESH -> R.string.argo_done_hard_refresh
        ArgoAction.SYNC -> R.string.argo_done_sync
        ArgoAction.TERMINATE -> R.string.argo_done_terminate
        ArgoAction.AUTO_SYNC_ON -> R.string.argo_done_auto_sync_on
        ArgoAction.AUTO_SYNC_OFF -> R.string.argo_done_auto_sync_off
        ArgoAction.ROLLBACK -> R.string.argo_done_rollback
    }
