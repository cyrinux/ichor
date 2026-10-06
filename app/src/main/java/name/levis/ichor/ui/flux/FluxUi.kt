package name.levis.ichor.ui.flux

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Autorenew
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.PauseCircle
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.ArgoOwner
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.FluxRef
import name.levis.ichor.model.FluxState
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.argocd.ArgoGlyph
import name.levis.ichor.ui.argocd.GitOpsPolling
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors

/** Green ready, blue reconciling, red failing, grey suspended. */
@Composable
fun FluxState.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        FluxState.READY -> colors.ok
        FluxState.RECONCILING -> MaterialTheme.colorScheme.primary
        FluxState.FAILING -> colors.bad
        FluxState.SUSPENDED -> colors.muted
    }
}

val FluxState.icon: ImageVector
    get() = when (this) {
        FluxState.READY -> Icons.Outlined.CheckCircle
        FluxState.RECONCILING -> Icons.Outlined.Autorenew
        FluxState.FAILING -> Icons.Outlined.ErrorOutline
        FluxState.SUSPENDED -> Icons.Outlined.PauseCircle
    }

/** The state in the user's language: counts and screen readers. */
@Composable
fun FluxState.label(): String = stringResource(
    when (this) {
        FluxState.READY -> R.string.flux_state_ready
        FluxState.RECONCILING -> R.string.flux_state_reconciling
        FluxState.FAILING -> R.string.flux_state_failing
        FluxState.SUSPENDED -> R.string.flux_state_suspended
    },
)

@Composable
fun FluxStateGlyph(state: FluxState, size: Dp = 18.dp) = ArgoGlyph(state.icon, state.color(), state.label(), size)

/** The app's catalog icon (the chart's, or one the Go core matched), or a monogram in its own colour. */
@Composable
fun FluxAppIcon(app: FluxApp, size: Dp, modifier: Modifier = Modifier) {
    val tile = InventoryApp(id = app.icon.ifEmpty { app.chart.ifEmpty { app.name } }, name = app.name, icon = app.icon, remoteIcon = app.remoteIcon)
    AppIconTile(tile, modifier, size = size)
}

/** For [name.levis.ichor.ui.argocd.OwnerChip]: the Kustomization applying an object. */
fun FluxRef.asArgoOwner(): ArgoOwner = ArgoOwner(kind, name)

/** While the screen is started, polls every 2 s when [FluxViewModel.shouldPoll] says so. */
@Composable
fun FluxPolling(vm: FluxViewModel) = GitOpsPolling(vm, { vm.shouldPoll }, vm::poll)

/** The outcome of each action as a snackbar message, through [show]. */
@Composable
fun FluxActionMessages(results: Flow<FluxActionResult>, show: suspend (String) -> Unit) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r ->
            show(r.error?.let { context.getString(R.string.argo_action_failed, it.resolve(context)) } ?: context.getString(r.action.doneLabel, r.name))
        }
    }
}

/** "Reconcile requested for apps"... */
private val FluxAction.doneLabel: Int
    get() = when (this) {
        FluxAction.RECONCILE -> R.string.flux_done_reconcile
        FluxAction.RECONCILE_WITH_SOURCE -> R.string.flux_done_reconcile_with_source
        FluxAction.SUSPEND -> R.string.flux_done_suspend
        FluxAction.RESUME -> R.string.flux_done_resume
        FluxAction.FORCE -> R.string.flux_done_force
        FluxAction.RESET -> R.string.flux_done_reset
    }

val FluxAction.label: Int
    get() = when (this) {
        FluxAction.RECONCILE -> R.string.flux_reconcile
        FluxAction.RECONCILE_WITH_SOURCE -> R.string.flux_reconcile_with_source
        FluxAction.SUSPEND -> R.string.flux_suspend
        FluxAction.RESUME -> R.string.flux_resume
        FluxAction.FORCE -> R.string.flux_force
        FluxAction.RESET -> R.string.flux_reset
    }

/** A Flux action waiting for its confirmation: on which object, and who applies it from Git ([owner], "" when none). */
data class FluxConfirm(val action: FluxAction, val kind: String, val namespace: String, val name: String, val owner: String = "")

/**
 * Asks before [confirm.action], saying what it does. Suspending or resuming an object a
 * Kustomization applies from Git warns that a suspend set in Git wins at that one's next reconcile.
 */
@Composable
fun FluxConfirmDialog(confirm: FluxConfirm, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val (title, text) = when (confirm.action) {
        FluxAction.RECONCILE -> R.string.flux_reconcile_title to R.string.flux_reconcile_text
        FluxAction.RECONCILE_WITH_SOURCE -> R.string.flux_reconcile_with_source_title to R.string.flux_reconcile_with_source_text
        FluxAction.SUSPEND -> R.string.flux_suspend_title to R.string.flux_suspend_text
        FluxAction.RESUME -> R.string.flux_resume_title to R.string.flux_resume_text
        FluxAction.FORCE -> R.string.flux_force_title to R.string.flux_force_text
        FluxAction.RESET -> R.string.flux_reset_title to R.string.flux_reset_text
    }
    val gitWins = confirm.owner.isNotEmpty() && (confirm.action == FluxAction.SUSPEND || confirm.action == FluxAction.RESUME)
    ConfirmDialog(
        title = stringResource(title, confirm.name),
        text = stringResource(text) + if (gitWins) "\n\n" + stringResource(R.string.flux_owner_wins, confirm.owner) else "",
        confirm = stringResource(confirm.action.label),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
        destructive = confirm.action == FluxAction.SUSPEND,
    )
}
