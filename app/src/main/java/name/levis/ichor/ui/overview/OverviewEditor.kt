package name.levis.ichor.ui.overview

import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.DragHandle
import androidx.compose.material.icons.outlined.VisibilityOff
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
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import name.levis.ichor.R
import name.levis.ichor.model.OverviewCard
import name.levis.ichor.model.OverviewBar
import name.levis.ichor.model.OverviewLayout
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton

private val ROW_SPACING = 8.dp

/**
 * Calls [onLongPress] when a finger rests on the element, before its children see the press
 * (cards are clickable themselves); the rest of that touch is then swallowed so the card under
 * it does not open. A move, a scroll or lifting the finger first leaves the touch to the children.
 */
fun Modifier.longPressToCustomize(onLongPress: () -> Unit): Modifier = pointerInput(Unit) {
    awaitEachGesture {
        val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        val released = withTimeoutOrNull(viewConfiguration.longPressTimeoutMillis) {
            var ended = false
            while (!ended) {
                val change = awaitPointerEvent(PointerEventPass.Initial).changes.firstOrNull { it.id == down.id }
                ended = change == null || !change.pressed || change.isConsumed ||
                    (change.position - down.position).getDistance() > viewConfiguration.touchSlop
            }
        }
        if (released != null) return@awaitEachGesture
        onLongPress()
        do {
            val event = awaitPointerEvent(PointerEventPass.Initial)
            event.changes.forEach { it.consume() }
        } while (event.changes.any { it.pressed })
    }
}

/** Enters the customize mode with a haptic tick. */
@Composable
fun rememberCustomizeTrigger(onCustomize: () -> Unit): () -> Unit {
    val haptics = LocalHapticFeedback.current
    val latest by rememberUpdatedState(onCustomize)
    return remember(haptics) {
        {
            haptics.performHapticFeedback(HapticFeedbackType.LongPress)
            latest()
        }
    }
}

@Composable
fun overviewCardLabel(card: OverviewCard): String = stringResource(
    when (card) {
        OverviewCard.TALOS_UPDATE -> R.string.overview_card_talos_update
        OverviewCard.SUMMARY -> R.string.overview_card_summary
        OverviewCard.APPS -> R.string.apps_title
        OverviewCard.DATA_SERVICES -> R.string.data_services_title
        OverviewCard.ARGO_CD -> R.string.argo_card_title
        OverviewCard.FLUX -> R.string.flux_title
        OverviewCard.NODES -> R.string.overview_stat_nodes
        OverviewCard.TIME_DRIFT -> R.string.time_drift_title
    },
)

/** One line on what the card shows, so the editor's names need no guessing. */
@Composable
fun overviewCardDescription(card: OverviewCard): String = stringResource(
    when (card) {
        OverviewCard.TALOS_UPDATE -> R.string.overview_card_desc_talos_update
        OverviewCard.SUMMARY -> R.string.overview_card_desc_summary
        OverviewCard.APPS -> R.string.overview_card_desc_apps
        OverviewCard.DATA_SERVICES -> R.string.overview_card_desc_data_services
        OverviewCard.ARGO_CD -> R.string.overview_card_desc_argo_cd
        OverviewCard.FLUX -> R.string.overview_card_desc_flux
        OverviewCard.NODES -> R.string.overview_card_desc_nodes
        OverviewCard.TIME_DRIFT -> R.string.overview_card_desc_time_drift
    },
)

/**
 * The overview's app bar and cards to arrange. Cards: drag a shown card by its handle to move it,
 * hide it, or add a hidden one back (at the end). Cards the cluster lacks ([absent]: no Argo CD,
 * no Flux...) are not offered. Every change is applied at once through [onChange] and [onBarChange].
 */
@Composable
fun OverviewEditor(
    layout: OverviewLayout,
    onChange: (OverviewLayout) -> Unit,
    bar: OverviewBar,
    onBarChange: (OverviewBar) -> Unit,
    modifier: Modifier = Modifier,
    absent: Set<OverviewCard> = emptySet(),
) {
    val current by rememberUpdatedState(layout)
    val currentAbsent by rememberUpdatedState(absent)
    val shownCards = layout.visible(absent)
    val hiddenCards = layout.hiddenCards(absent)
    val change by rememberUpdatedState(onChange)
    val currentBar by rememberUpdatedState(bar)
    val changeBar by rememberUpdatedState(onBarChange)
    val barDrag = remember { BarDragState() }
    var dragged by remember { mutableStateOf<OverviewCard?>(null) }
    var dragOffset by remember { mutableFloatStateOf(0f) }
    // Row heights, to know when the dragged card passed its neighbour's middle.
    val heights = remember { mutableStateMapOf<OverviewCard, Int>() }
    val spacing = with(LocalDensity.current) { ROW_SPACING.toPx() }

    fun dragBy(card: OverviewCard, delta: Float) {
        dragOffset += delta
        val shown = current.visible(currentAbsent)
        val index = shown.indexOf(card)
        val next = shown.getOrNull(index + 1)
        val previous = shown.getOrNull(index - 1)
        val down = next?.let { (heights[it] ?: 0) + spacing }
        val up = previous?.let { (heights[it] ?: 0) + spacing }
        when {
            down != null && dragOffset > down / 2 -> {
                change(current.move(index, index + 1, currentAbsent))
                dragOffset -= down
            }
            up != null && dragOffset < -up / 2 -> {
                change(current.move(index, index - 1, currentAbsent))
                dragOffset += up
            }
        }
    }

    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(ROW_SPACING),
        modifier = modifier.fillMaxSize(),
    ) {
        overviewBarItems(bar, { currentBar }, { changeBar(it) }, barDrag, spacing)
        item(key = "hint") {
            SectionTitle(stringResource(R.string.overview_edit_cards), stringResource(R.string.overview_edit_hint), Modifier.padding(top = 16.dp))
        }
        itemsIndexed(shownCards, key = { _, card -> card.name }) { index, card ->
            val isDragged = dragged == card
            val moveUp = stringResource(R.string.overview_edit_move_up)
            val moveDown = stringResource(R.string.overview_edit_move_down)
            ShownCardRow(
                card = card,
                onHide = { change(current.hide(card)) },
                handle = Modifier.pointerInput(card) {
                    detectVerticalDragGestures(
                        onDragStart = {
                            dragged = card
                            dragOffset = 0f
                        },
                        onDragEnd = { dragged = null },
                        onDragCancel = { dragged = null },
                    ) { pointer, delta ->
                        pointer.consume()
                        dragBy(card, delta)
                    }
                },
                modifier = Modifier
                    .onSizeChanged { heights[card] = it.height }
                    .then(
                        if (isDragged) Modifier.zIndex(1f).graphicsLayer { translationY = dragOffset }
                        else Modifier.animateItem(),
                    )
                    .semantics {
                        customActions = listOfNotNull(
                            CustomAccessibilityAction(moveUp) { change(current.move(index, index - 1, currentAbsent)); true }.takeIf { index > 0 },
                            CustomAccessibilityAction(moveDown) { change(current.move(index, index + 1, currentAbsent)); true }
                                .takeIf { index < shownCards.lastIndex },
                        )
                    },
                lifted = isDragged,
            )
        }
        if (hiddenCards.isNotEmpty()) item(key = "hidden-title") {
            Text(
                stringResource(R.string.overview_edit_hidden),
                style = MaterialTheme.typography.titleSmall,
                modifier = Modifier.padding(top = 16.dp).animateItem(),
            )
        }
        items(hiddenCards, key = { it.name }) { card ->
            HiddenCardRow(card, onShow = { change(current.show(card)) }, modifier = Modifier.animateItem())
        }
        if (!layout.isDefault) item(key = "reset") {
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth().animateItem()) {
                TextButton(onClick = { change(OverviewLayout()) }) { Text(stringResource(R.string.overview_edit_reset)) }
            }
        }
    }
}

@Composable
private fun ShownCardRow(
    card: OverviewCard,
    onHide: () -> Unit,
    handle: Modifier,
    modifier: Modifier,
    lifted: Boolean,
) {
    Card(
        modifier = modifier.fillMaxWidth(),
        elevation = CardDefaults.cardElevation(defaultElevation = if (lifted) 8.dp else 0.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(start = 4.dp, end = 4.dp)) {
            Icon(
                Icons.Outlined.DragHandle,
                contentDescription = stringResource(R.string.overview_edit_drag),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = handle.padding(12.dp),
            )
            CardName(card, Modifier.weight(1f))
            TooltipIconButton(Icons.Outlined.VisibilityOff, stringResource(R.string.overview_edit_hide), onClick = onHide)
        }
    }
}

@Composable
private fun HiddenCardRow(card: OverviewCard, onShow: () -> Unit, modifier: Modifier) {
    Card(modifier = modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(start = 16.dp, end = 4.dp)) {
            CardName(card, Modifier.weight(1f).alpha(0.6f))
            TooltipIconButton(Icons.Outlined.Add, stringResource(R.string.overview_edit_show), onClick = onShow)
        }
    }
}

@Composable
private fun CardName(card: OverviewCard, modifier: Modifier) {
    Column(modifier.padding(vertical = 12.dp)) {
        Text(overviewCardLabel(card), style = MaterialTheme.typography.titleMedium)
        MutedText(overviewCardDescription(card))
        if (card.whenDetected) MutedText(stringResource(R.string.overview_edit_when_detected))
    }
}
