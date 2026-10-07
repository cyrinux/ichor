package name.levis.ichor.ui.components

import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.DragHandle
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import name.levis.ichor.R
import name.levis.ichor.model.ActionBar

private const val MENU_LINE = "bar-menu"
private val ROW_SPACING = 8.dp

private fun slot(action: Enum<*>) = "bar-${action.name}"

/** How the editor shows an action: its icon and name, as in the bar. */
class ActionLook<A : Enum<A>>(val icon: (A) -> ImageVector, val label: @Composable (A) -> String)

/** The action being dragged in the bar section, and the row heights to know when it passed one. */
class BarDragState {
    var dragged by mutableStateOf<Enum<*>?>(null)
    var offset by mutableFloatStateOf(0f)
    val heights = mutableStateMapOf<String, Int>()

    /**
     * Moves [action] one row once dragged past its neighbour's middle; the menu line counts as a
     * row, so passing it moves the action between the icons and the menu.
     */
    fun <A : Enum<A>> dragBy(action: A, delta: Float, bar: ActionBar<A>, spacing: Float, onChange: (ActionBar<A>) -> Unit) {
        offset += delta
        val slots = bar.icons.map(::slot) + MENU_LINE + bar.menu.map(::slot)
        val index = slots.indexOf(slot(action))
        val down = slots.getOrNull(index + 1)?.let { (heights[it] ?: 0) + spacing }
        val up = slots.getOrNull(index - 1)?.let { (heights[it] ?: 0) + spacing }
        when {
            down != null && offset > down / 2 -> {
                onChange(bar.down(action))
                offset -= down
            }
            up != null && offset < -up / 2 -> {
                onChange(bar.up(action))
                offset += up
            }
        }
    }
}

/**
 * The app bar's actions to arrange: icons first, then a line, then those in its menu. Drag an
 * action by its handle across the line, or use its arrow, to move it between the two.
 */
fun <A : Enum<A>> LazyListScope.actionBarItems(
    bar: ActionBar<A>,
    look: ActionLook<A>,
    current: () -> ActionBar<A>,
    onChange: (ActionBar<A>) -> Unit,
    drag: BarDragState,
    spacing: Float,
) {
    item(key = "bar-title") {
        SectionTitle(stringResource(R.string.overview_edit_bar), stringResource(R.string.overview_edit_bar_hint))
    }
    items(bar.icons, key = ::slot) { action -> BarRow(action, look, inMenu = false, current, onChange, drag, spacing) }
    item(key = MENU_LINE) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.onSizeChanged { drag.heights[MENU_LINE] = it.height }.padding(top = 8.dp).animateItem(),
        ) {
            Icon(Icons.Outlined.MoreVert, contentDescription = null, Modifier.size(18.dp))
            Text(stringResource(R.string.overview_edit_menu), style = MaterialTheme.typography.titleSmall)
        }
    }
    items(bar.menu, key = ::slot) { action -> BarRow(action, look, inMenu = true, current, onChange, drag, spacing) }
    if (!bar.isDefault) item(key = "bar-reset") {
        Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth().animateItem()) {
            TextButton(onClick = { onChange(bar.kind.default) }) { Text(stringResource(R.string.overview_edit_reset)) }
        }
    }
}

/** A section's title with a line on how to use it. */
@Composable
fun SectionTitle(title: String, hint: String, modifier: Modifier = Modifier) {
    Column(modifier.padding(bottom = 8.dp)) {
        Text(title, style = MaterialTheme.typography.titleSmall)
        MutedText(hint)
    }
}

/**
 * A screen's whole editor for its app bar (only [actionBarItems]), shown in place of the
 * screen's content while it is customized. Every change is applied at once through [onChange].
 */
@Composable
fun <A : Enum<A>> ActionBarEditor(bar: ActionBar<A>, look: ActionLook<A>, onChange: (ActionBar<A>) -> Unit, modifier: Modifier = Modifier) {
    val current by rememberUpdatedState(bar)
    val change by rememberUpdatedState(onChange)
    val drag = remember { BarDragState() }
    val spacing = with(LocalDensity.current) { ROW_SPACING.toPx() }
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(ROW_SPACING),
        modifier = modifier.fillMaxSize(),
    ) {
        actionBarItems(bar, look, { current }, { change(it) }, drag, spacing)
    }
}

@Composable
private fun <A : Enum<A>> LazyItemScope.BarRow(
    action: A,
    look: ActionLook<A>,
    inMenu: Boolean,
    current: () -> ActionBar<A>,
    onChange: (ActionBar<A>) -> Unit,
    drag: BarDragState,
    spacing: Float,
) {
    val lifted = drag.dragged == action
    val moveUp = stringResource(R.string.overview_edit_move_up)
    val moveDown = stringResource(R.string.overview_edit_move_down)
    Card(
        elevation = CardDefaults.cardElevation(defaultElevation = if (lifted) 8.dp else 0.dp),
        modifier = Modifier
            .fillMaxWidth()
            .onSizeChanged { drag.heights[slot(action)] = it.height }
            .then(if (lifted) Modifier.zIndex(1f).graphicsLayer { translationY = drag.offset } else Modifier.animateItem())
            .semantics {
                val order = current().order
                customActions = listOfNotNull(
                    CustomAccessibilityAction(moveUp) { onChange(current().up(action)); true }.takeIf { order.first() != action },
                    CustomAccessibilityAction(moveDown) { onChange(current().down(action)); true }.takeIf { order.last() != action },
                )
            },
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(horizontal = 4.dp)) {
            Icon(
                Icons.Outlined.DragHandle,
                contentDescription = stringResource(R.string.overview_edit_drag),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier
                    .pointerInput(action) {
                        detectVerticalDragGestures(
                            onDragStart = {
                                drag.dragged = action
                                drag.offset = 0f
                            },
                            onDragEnd = { drag.dragged = null },
                            onDragCancel = { drag.dragged = null },
                        ) { pointer, delta ->
                            pointer.consume()
                            drag.dragBy(action, delta, current(), spacing, onChange)
                        }
                    }
                    .padding(12.dp),
            )
            Icon(look.icon(action), contentDescription = null, Modifier.padding(end = 12.dp))
            Text(look.label(action), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f).padding(vertical = 12.dp))
            if (inMenu) {
                TooltipIconButton(Icons.Outlined.ArrowUpward, stringResource(R.string.overview_edit_to_bar), onClick = { onChange(current().toBar(action)) })
            } else {
                TooltipIconButton(Icons.Outlined.ArrowDownward, stringResource(R.string.overview_edit_to_menu), onClick = { onChange(current().toMenu(action)) })
            }
        }
    }
}
