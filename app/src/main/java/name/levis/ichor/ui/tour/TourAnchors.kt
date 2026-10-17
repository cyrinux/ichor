package name.levis.ichor.ui.tour

import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.boundsInRoot
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp

private const val MAX_REVEAL_SCROLLS = 12
private const val PAGE_SHARE = 0.8f
private val REVEAL_MARGIN = 16.dp

/**
 * Where each anchor of the tour is on screen (root coordinates), while it is laid out, and how
 * a list brings one of its anchors into view (a card below the fold is not laid out yet).
 */
class TourAnchors {
    val bounds = mutableStateMapOf<TourAnchor, Rect>()

    /** Anchors a list can scroll to although they are not on screen now. */
    var reachable by mutableStateOf(emptySet<TourAnchor>())

    internal var revealer: (suspend (TourAnchor) -> Unit)? = null

    /** Scrolls the list holding [anchor] until it is on screen, if a list holds it. */
    suspend fun reveal(anchor: TourAnchor) {
        revealer?.invoke(anchor)
    }
}

/** The tour's anchors on the screen hosting it; null elsewhere, where [tourAnchor] does nothing. */
val LocalTourAnchors = staticCompositionLocalOf<TourAnchors?> { null }

/** Marks this element as what the tour's [anchor] step points at (bounds resolved as it is laid out). */
fun Modifier.tourAnchor(anchor: TourAnchor): Modifier = composed {
    val anchors = LocalTourAnchors.current ?: return@composed Modifier
    DisposableEffect(anchors, anchor) { onDispose { anchors.bounds.remove(anchor) } }
    Modifier.onGloballyPositioned { coordinates ->
        val bounds = coordinates.boundsInRoot()
        if (anchors.bounds[anchor] != bounds) anchors.bounds[anchor] = bounds
    }
}

/** On the list [state] scrolls, holding the [reachable] anchors: the tour scrolls to them. */
fun Modifier.tourScrollable(state: LazyListState, reachable: Set<TourAnchor>): Modifier = composed {
    val anchors = LocalTourAnchors.current ?: return@composed Modifier
    val margin = with(LocalDensity.current) { REVEAL_MARGIN.toPx() }
    val viewport = remember { mutableStateOf(Rect.Zero) }
    LaunchedEffect(anchors, reachable) { anchors.reachable = reachable }
    DisposableEffect(anchors, state) {
        anchors.revealer = { anchor -> if (anchor in anchors.reachable) state.reveal(anchors, anchor, { viewport.value }, margin) }
        onDispose {
            anchors.revealer = null
            anchors.reachable = emptySet()
        }
    }
    Modifier.onGloballyPositioned { viewport.value = it.boundsInRoot() }
}

/** Scrolls until [anchor]'s top sits [margin] under the list's top, or the list ends. */
private suspend fun LazyListState.reveal(anchors: TourAnchors, anchor: TourAnchor, viewport: () -> Rect, margin: Float) {
    repeat(MAX_REVEAL_SCROLLS) {
        val list = viewport()
        val bounds = anchors.bounds[anchor]
        // Not laid out yet: a page further down. Laid out: up to its top.
        val delta = if (bounds == null || bounds.isEmpty) list.height * PAGE_SHARE else bounds.top - list.top - margin
        val stuck = (delta > 0 && !canScrollForward) || (delta < 0 && !canScrollBackward)
        if (kotlin.math.abs(delta) < 1f || stuck) return
        scrollBy(delta)
        withFrameNanos { }
    }
}
