package name.levis.ichor.ui.components

import android.annotation.SuppressLint
import android.os.Build
import androidx.activity.BackEventCompat
import androidx.activity.compose.BackHandler
import androidx.activity.compose.PredictiveBackHandler
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlin.coroutines.cancellation.CancellationException

/** How far a back swipe on a layer has gone: [progress] from 0 to 1, and the edge it started from. */
@Immutable
data class BackPreview(val progress: Float = 0f, val fromLeftEdge: Boolean = true) {
    companion object {
        /** No swipe in flight: the layer is drawn as it is. */
        val Idle = BackPreview()
    }
}

/** How much smaller a layer gets at the end of a back swipe, as a share of its size. */
private const val PREVIEW_SHRINK = 0.1f

/** How far a layer moves toward the swipe's far edge at the end of a back swipe, as a share of its width. */
private const val PREVIEW_SHIFT = 0.08f

/**
 * Follows one back gesture: each event of [events] is shown through [onPreview]; once the flow
 * completes (the swipe was released past the threshold, or a key or button press) [onBack] closes
 * the layer and the preview goes back to [BackPreview.Idle]. A cancelled swipe restores the layer
 * and rethrows, as [PredictiveBackHandler] requires, so back keeps working on the screen.
 */
internal suspend fun followBackGesture(events: Flow<BackPreview>, onPreview: (BackPreview) -> Unit, onBack: () -> Unit) {
    try {
        events.collect { onPreview(it) }
    } catch (e: CancellationException) {
        onPreview(BackPreview.Idle)
        throw e
    }
    onBack()
    onPreview(BackPreview.Idle)
}

private fun BackEventCompat.toPreview() =
    BackPreview(progress = progress.coerceIn(0f, 1f), fromLeftEdge = swipeEdge != BackEventCompat.EDGE_RIGHT)

/**
 * Back closes a layer of the screen (a detail pane, search, a selection or customise mode) and,
 * on Android 14 and later, the layer follows the finger while the swipe is in flight: give the
 * returned state to [predictiveBackPreview] on that layer. Below Android 14 it is a plain
 * [BackHandler] and the state stays idle. Keyboard and button back close the same layer.
 *
 * Back that asks first (unsaved edits) or blocks stays a plain [BackHandler] on purpose: a
 * preview would promise a close that does not happen.
 */
// followBackGesture collects the flow to completion; lint only looks for collect() in the lambda.
@SuppressLint("NoCollectCallFound")
@Composable
fun rememberPredictiveBack(enabled: Boolean = true, onBack: () -> Unit): State<BackPreview> {
    val preview = remember { mutableStateOf(BackPreview.Idle) }
    val currentOnBack by rememberUpdatedState(onBack)
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
        BackHandler(enabled) { currentOnBack() }
    } else {
        PredictiveBackHandler(enabled) { events ->
            followBackGesture(events.map { it.toPreview() }, onPreview = { preview.value = it }, onBack = { currentOnBack() })
        }
    }
    return preview
}

/**
 * Draws the layer as a back swipe previews its close: slightly smaller and moved toward the far
 * edge as [preview]'s progress grows, back in place when the swipe is cancelled.
 */
fun Modifier.predictiveBackPreview(preview: State<BackPreview>): Modifier = graphicsLayer {
    val back = preview.value
    val scale = 1f - PREVIEW_SHRINK * back.progress
    scaleX = scale
    scaleY = scale
    val shift = size.width * PREVIEW_SHIFT * back.progress
    translationX = if (back.fromLeftEdge) shift else -shift
    transformOrigin = TransformOrigin.Center
}
