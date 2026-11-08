package name.levis.ichor.ui.components

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R

/**
 * One thing a list row can do: on a swipe edge, in its long-press menu, and as a TalkBack
 * custom action. [enabled] false draws it dimmed with [disabledReason] under it (a can-I
 * denial, `KubePermission.denialText()`). A [destructive] action's [onClick] opens its
 * confirmation; nothing runs on the swipe's release.
 */
data class RowAction(
    val icon: ImageVector,
    val label: String,
    val onClick: () -> Unit,
    val enabled: Boolean = true,
    val disabledReason: String? = null,
    val destructive: Boolean = false,
    val tint: Color = Color.Unspecified,
)

/** Which edges of a row can be dragged: from the start (the leading action), from the end. */
data class SwipeEdges(val fromStart: Boolean, val fromEnd: Boolean)

/** An edge drags only with an enabled action behind it, and none while [swipeEnabled] is false. */
internal fun swipeEdges(leading: RowAction?, trailing: RowAction?, swipeEnabled: Boolean) = SwipeEdges(
    fromStart = swipeEnabled && leading?.enabled == true,
    fromEnd = swipeEnabled && trailing?.enabled == true,
)

/**
 * What TalkBack offers on the row: the swipe edges (while swiping is on) and the menu, enabled
 * ones only, one per label (TalkBack merges two actions with the same label).
 */
internal fun accessibleActions(
    leading: RowAction?,
    trailing: RowAction?,
    menu: List<RowAction>,
    swipeEnabled: Boolean = true,
): List<RowAction> =
    (listOfNotNull(leading, trailing).filter { swipeEnabled } + menu)
        .filter { it.enabled }
        .distinctBy { it.label }

/** The colour family of a swipe background. */
enum class SwipeTone { PRIMARY, TERTIARY, ERROR }

/** Leading reads as primary, trailing as tertiary, and a destructive action as an error either side. */
internal fun swipeTone(action: RowAction, leading: Boolean): SwipeTone = when {
    action.destructive -> SwipeTone.ERROR
    leading -> SwipeTone.PRIMARY
    else -> SwipeTone.TERTIARY
}

/**
 * A list row with quick actions. Swipe right runs [leading], swipe left [trailing]: the row ticks
 * when the swipe passes its threshold, runs the action once and always springs back. A long
 * press opens [menu] as a dropdown (none when it is empty), and TalkBack lists every action.
 * [swipeEnabled] false (a request in flight, multi-select) stops both edges.
 *
 * With [onClick] null and [menu] empty the row adds no click of its own: [content] keeps its
 * clicks, long press and accessibility actions (the Talos node row). [shape]: the swipe
 * background's corners, to match a rounded row.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QuickActionRow(
    leading: RowAction?,
    trailing: RowAction?,
    menu: List<RowAction>,
    onClick: (() -> Unit)?,
    modifier: Modifier = Modifier,
    swipeEnabled: Boolean = true,
    onClickLabel: String? = null,
    shape: Shape = RectangleShape,
    menuHeader: String? = null,
    content: @Composable () -> Unit,
) {
    val haptics = LocalHapticFeedback.current
    val state = rememberSwipeToDismissBoxState()
    val edges = swipeEdges(leading, trailing, swipeEnabled)
    val currentLeading by rememberUpdatedState(leading)
    val currentTrailing by rememberUpdatedState(trailing)
    // The tick: the swipe has passed the point where letting go runs the action.
    LaunchedEffect(state.targetValue) {
        if (state.targetValue != SwipeToDismissBoxValue.Settled) {
            haptics.performHapticFeedback(HapticFeedbackType.GestureThresholdActivate)
        }
    }
    LaunchedEffect(state.currentValue) {
        val action = when (state.currentValue) {
            SwipeToDismissBoxValue.StartToEnd -> currentLeading
            SwipeToDismissBoxValue.EndToStart -> currentTrailing
            SwipeToDismissBoxValue.Settled -> return@LaunchedEffect
        }
        if (action != null && action.enabled) {
            action.onClick()
            if (!action.destructive) haptics.performHapticFeedback(HapticFeedbackType.Confirm)
        }
        state.reset() // spring back, whatever ran
    }
    SwipeToDismissBox(
        state = state,
        modifier = modifier,
        enableDismissFromStartToEnd = edges.fromStart,
        enableDismissFromEndToStart = edges.fromEnd,
        backgroundContent = { SwipeBackground(state.dismissDirection, leading, trailing, shape) },
    ) {
        RowForeground(leading, trailing, menu, menuHeader, swipeEnabled, onClick, onClickLabel, content)
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun RowForeground(
    leading: RowAction?,
    trailing: RowAction?,
    menu: List<RowAction>,
    menuHeader: String?,
    swipeEnabled: Boolean,
    onClick: (() -> Unit)?,
    onClickLabel: String?,
    content: @Composable () -> Unit,
) {
    val haptics = LocalHapticFeedback.current
    var menuOpen by remember { mutableStateOf(false) }
    val clicks = when {
        menu.isNotEmpty() -> Modifier.combinedClickable(
            onClick = onClick ?: { menuOpen = true },
            onClickLabel = onClickLabel,
            onLongClick = {
                haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                menuOpen = true
            },
            onLongClickLabel = stringResource(R.string.row_actions),
        )
        onClick != null -> Modifier.clickable(onClickLabel = onClickLabel, onClick = onClick)
        else -> null
    }
    // Only on the row's own click target: a second focusable node would read the row twice.
    val semantics = clicks?.let {
        val actions = accessibleActions(leading, trailing, menu, swipeEnabled)
        Modifier.semantics {
            customActions = actions.map { action -> CustomAccessibilityAction(action.label) { action.onClick(); true } }
        }
    }
    Box(Modifier.heightIn(min = 48.dp).then(clicks ?: Modifier).then(semantics ?: Modifier)) {
        content()
        if (menu.isNotEmpty()) RowActionsMenu(menuOpen, menu, onDismiss = { menuOpen = false }, header = menuHeader)
    }
}

/** A row's actions as a dropdown, under [header] when set (a full name the row may cut short). */
@Composable
internal fun RowActionsMenu(expanded: Boolean, actions: List<RowAction>, onDismiss: () -> Unit, header: String? = null) {
    DropdownMenu(expanded = expanded, onDismissRequest = onDismiss) {
        header?.let {
            Text(
                it,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
        actions.forEach { action -> RowActionItem(action, onDismiss) }
    }
}

@Composable
private fun RowActionItem(action: RowAction, close: () -> Unit) {
    val dimmed = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f)
    val color = when {
        !action.enabled -> dimmed
        action.destructive -> MaterialTheme.colorScheme.error
        action.tint != Color.Unspecified -> action.tint
        else -> MaterialTheme.colorScheme.onSurface
    }
    DropdownMenuItem(
        text = {
            Column {
                Text(action.label, color = color)
                if (!action.enabled) action.disabledReason?.let {
                    Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        },
        leadingIcon = { Icon(action.icon, contentDescription = null, tint = color) },
        enabled = action.enabled,
        onClick = {
            close()
            action.onClick()
        },
    )
}

@Composable
private fun SwipeBackground(direction: SwipeToDismissBoxValue, leading: RowAction?, trailing: RowAction?, shape: Shape) {
    val isLeading = direction == SwipeToDismissBoxValue.StartToEnd
    val action = when (direction) {
        SwipeToDismissBoxValue.StartToEnd -> leading
        SwipeToDismissBoxValue.EndToStart -> trailing
        SwipeToDismissBoxValue.Settled -> null
    } ?: return
    val scheme = MaterialTheme.colorScheme
    val (container, onContainer) = when (swipeTone(action, isLeading)) {
        SwipeTone.PRIMARY -> scheme.primaryContainer to scheme.onPrimaryContainer
        SwipeTone.TERTIARY -> scheme.tertiaryContainer to scheme.onTertiaryContainer
        SwipeTone.ERROR -> scheme.errorContainer to scheme.onErrorContainer
    }
    Box(
        Modifier.fillMaxSize().clip(shape).background(container).padding(horizontal = 24.dp),
        contentAlignment = if (isLeading) Alignment.CenterStart else Alignment.CenterEnd,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(action.icon, contentDescription = null, tint = onContainer)
            Text(action.label, color = onContainer, style = MaterialTheme.typography.labelLarge)
        }
    }
}
