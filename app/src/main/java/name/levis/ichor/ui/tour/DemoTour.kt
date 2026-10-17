package name.levis.ichor.ui.tour

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.boundsInRoot
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.traversalIndex
import androidx.compose.ui.unit.coerceAtMost
import androidx.compose.ui.unit.dp
import name.levis.ichor.R

private const val SCRIM_ALPHA = 0.6f
private const val MAX_OFFSET_SHARE = 0.6f
private val CUTOUT_MARGIN = 6.dp
private val CUTOUT_CORNER = 12.dp
private val CARD_GAP = 12.dp
private val CARD_MAX_WIDTH = 480.dp
private val DOT_SIZE = 6.dp

/** Hosts [content] with its tour anchors, and the tour over it while [active]. */
@Composable
fun DemoTourHost(active: Boolean, onFinish: () -> Unit, content: @Composable () -> Unit) {
    val anchors = remember { TourAnchors() }
    CompositionLocalProvider(LocalTourAnchors provides anchors) {
        Box(Modifier.fillMaxSize()) {
            content()
            if (active) TourOverlay(demoTourSteps, anchors, onFinish)
        }
    }
}

/**
 * A scrim over the screen with a cut-out around the current step's anchor and a card saying
 * what it is. Steps whose anchor is not on screen are left out; Skip (first for TalkBack) and
 * Done both end the tour through [onFinish], as does Back.
 */
@Composable
fun TourOverlay(steps: List<TourStep>, anchors: TourAnchors, onFinish: () -> Unit) {
    var current by rememberSaveable { mutableStateOf(0) }
    // The step whose anchor was scrolled into view, then laid out; none is drawn before.
    var revealed by remember { mutableStateOf(-1) }
    var viewport by remember { mutableStateOf(Rect.Zero) }
    BackHandler(onBack = onFinish)
    // A list scrolls to the anchor first; one still absent after that is skipped.
    LaunchedEffect(current) {
        steps.getOrNull(current)?.let { anchors.reveal(it.anchor) }
        withFrameNanos { }
        revealed = current
    }
    Box(
        Modifier
            .fillMaxSize()
            .onGloballyPositioned { viewport = it.boundsInRoot() }
            // Taps on the scrim go nowhere: the home under it waits for the tour.
            .pointerInput(Unit) { detectTapGestures { } },
    ) {
        // Nothing is known to be on screen before the overlay itself is laid out.
        if (viewport.isEmpty) return@Box
        val present = presentAnchors(anchors.bounds, viewport)
        // Numbered among those on screen or that a list scrolls to.
        val available = present + anchors.reachable
        val position = tourPosition(steps, current, available)
        if (position == null) {
            // Nothing left to point at: never an invisible layer holding the taps.
            SideEffect { onFinish() }
            return@Box
        }
        if (position.step != current) {
            SideEffect { current = position.step }
            return@Box
        }
        if (revealed != current) return@Box
        val step = steps[position.step]
        val target = anchors.bounds[step.anchor]?.takeIf { step.anchor in present }?.translate(-viewport.left, -viewport.top)
        if (target == null) {
            // Not on screen even after scrolling: on to the next step, or the end.
            SideEffect { if (tourPosition(steps, current + 1, available) == null) onFinish() else current += 1 }
            return@Box
        }
        Scrim(target)
        TourCard(
            step = step,
            position = position,
            target = target,
            height = viewport.height,
            onSkip = onFinish,
            onNext = {
                val next = tourPosition(steps, position.step + 1, available)
                if (next == null) onFinish() else current = next.step
            },
        )
    }
}

@Composable
private fun Scrim(target: Rect) {
    val scrim = MaterialTheme.colorScheme.scrim.copy(alpha = SCRIM_ALPHA)
    val density = LocalDensity.current
    val margin = with(density) { CUTOUT_MARGIN.toPx() }
    val corner = with(density) { CUTOUT_CORNER.toPx() }
    Canvas(Modifier.fillMaxSize().graphicsLayer(compositingStrategy = CompositingStrategy.Offscreen)) {
        drawRect(scrim)
        val hole = target.inflate(margin)
        drawRoundRect(Color.Transparent, hole.topLeft, hole.size, CornerRadius(corner), blendMode = BlendMode.Clear)
    }
}

/** The step's card, below its anchor when that is in the upper half of the screen, else above. */
@Composable
private fun TourCard(step: TourStep, position: TourPosition, target: Rect, height: Float, onSkip: () -> Unit, onNext: () -> Unit) {
    val density = LocalDensity.current
    val margin = with(density) { CUTOUT_MARGIN.toPx() }
    val below = target.center.y < height / 2
    // A tall anchor (the nodes card) leaves the card some room still, over the anchor's far end.
    val offset = with(density) {
        ((if (below) target.bottom + margin else height - target.top + margin).toDp() + CARD_GAP).coerceAtMost((height * MAX_OFFSET_SHARE).toDp())
    }
    Box(Modifier.fillMaxSize().padding(horizontal = 16.dp), contentAlignment = if (below) Alignment.TopCenter else Alignment.BottomCenter) {
        Card(
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh),
            elevation = CardDefaults.cardElevation(defaultElevation = 6.dp),
            modifier = Modifier
                .padding(top = if (below) offset else 0.dp, bottom = if (below) 0.dp else offset)
                .widthIn(max = CARD_MAX_WIDTH)
                .fillMaxWidth()
                .semantics { isTraversalGroup = true },
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        stringResource(R.string.tour_step_counter, position.number, position.total),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.weight(1f),
                    )
                    // Read first: a TalkBack user can leave the tour before anything else.
                    TextButton(onClick = onSkip, modifier = Modifier.semantics { traversalIndex = -1f }) {
                        Text(stringResource(R.string.tour_skip))
                    }
                }
                Column(Modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite }) {
                    Text(stringResource(step.title), style = MaterialTheme.typography.titleMedium)
                    Text(stringResource(step.text), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(top = 4.dp))
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    StepDots(position, Modifier.weight(1f))
                    Button(onClick = onNext) {
                        Text(stringResource(if (position.last) R.string.tour_done else R.string.tour_next))
                    }
                }
            }
        }
    }
}

/** A dot per step on screen, the current one filled; the counter above says it for TalkBack. */
@Composable
private fun StepDots(position: TourPosition, modifier: Modifier) {
    val on = MaterialTheme.colorScheme.primary
    val off = MaterialTheme.colorScheme.outlineVariant
    Row(modifier.clearAndSetSemantics { }, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        repeat(position.total) { i ->
            Spacer(Modifier.size(DOT_SIZE).background(if (i + 1 == position.number) on else off, CircleShape))
        }
    }
}
