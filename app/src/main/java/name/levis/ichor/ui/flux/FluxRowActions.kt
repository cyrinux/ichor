package name.levis.ichor.ui.flux

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.FluxSource
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.components.QuickActionRow
import name.levis.ichor.ui.components.RowAction
import name.levis.ichor.ui.components.denialReason
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.share.rememberPinAction
import name.levis.ichor.ui.share.rememberShareAction

/** What a Flux row may do now: its Suspend or Resume, and which of the two actions are on. */
internal data class FluxRowGate(val toggle: FluxAction, val canReconcile: Boolean, val canToggle: Boolean)

/** Flux refuses a reconcile on a suspended object; a request in flight or a denial stops both. */
internal fun fluxRowGate(suspended: Boolean, busy: Boolean, denied: Boolean): FluxRowGate = FluxRowGate(
    toggle = if (suspended) FluxAction.RESUME else FluxAction.SUSPEND,
    canReconcile = !suspended && !busy && !denied,
    canToggle = !busy && !denied,
)

/** [action] as a row action: its label, its [icon], dimmed with the [denial]'s reason when not [enabled]. */
@Composable
private fun fluxRowAction(action: FluxAction, icon: ImageVector, enabled: Boolean, denial: KubePermission?, onAct: (FluxAction) -> Unit) = RowAction(
    icon,
    stringResource(action.label),
    { onAct(action) },
    enabled = enabled,
    disabledReason = denial.denialReason(),
)

private val FluxRowGate.toggleIcon: ImageVector
    get() = if (toggle == FluxAction.RESUME) Icons.Outlined.PlayArrow else Icons.Outlined.Pause

/**
 * A Kustomization or HelmRelease row's quick actions: swipe right to reconcile, left to suspend or
 * resume, each through [onAct] (the confirmation first); a long press also lists Reconcile with
 * source, Pin and Share. A tap still opens the app ([onClick]).
 */
@Composable
internal fun FluxAppQuickActions(
    app: FluxApp,
    busy: Boolean,
    onClick: () -> Unit,
    onAct: (FluxAction) -> Unit,
    content: @Composable () -> Unit,
) {
    val denial = rememberKubeDenial(KubeAction.fluxReconcile(app.kind), app.namespace)
    val gate = fluxRowGate(app.suspended, busy, denial != null)
    val target = ShareTarget.fluxApp(app.kind, app.namespace, app.name)
    val reconcile = fluxRowAction(FluxAction.RECONCILE, Icons.Outlined.Refresh, gate.canReconcile, denial, onAct)
    val withSource = fluxRowAction(FluxAction.RECONCILE_WITH_SOURCE, Icons.Outlined.Sync, gate.canReconcile, denial, onAct)
    val toggle = fluxRowAction(gate.toggle, gate.toggleIcon, gate.canToggle, denial, onAct)
    QuickActionRow(
        leading = reconcile,
        trailing = toggle,
        menu = listOfNotNull(reconcile, withSource, toggle, rememberPinAction(target, app.name), rememberShareAction(target)),
        onClick = onClick,
        swipeEnabled = !busy,
        onClickLabel = stringResource(R.string.common_open),
        content = content,
    )
}

/**
 * A source row's quick actions: swipe right to reconcile, left to suspend or resume, each through
 * [onAct] (the confirmation first); a long press lists the two. The row's buttons stay.
 */
@Composable
internal fun FluxSourceQuickActions(
    source: FluxSource,
    busy: Boolean,
    onAct: (FluxAction) -> Unit,
    content: @Composable () -> Unit,
) {
    val denial = rememberKubeDenial(KubeAction.fluxReconcile(source.kind), source.namespace)
    val gate = fluxRowGate(source.suspended, busy, denial != null)
    val reconcile = fluxRowAction(FluxAction.RECONCILE, Icons.Outlined.Refresh, gate.canReconcile, denial, onAct)
    val toggle = fluxRowAction(gate.toggle, gate.toggleIcon, gate.canToggle, denial, onAct)
    QuickActionRow(
        leading = reconcile,
        trailing = toggle,
        menu = listOf(reconcile, toggle),
        onClick = null,
        swipeEnabled = !busy,
        content = content,
    )
}
