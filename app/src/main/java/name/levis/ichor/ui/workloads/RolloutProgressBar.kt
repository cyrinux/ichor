package name.levis.ichor.ui.workloads

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import name.levis.ichor.ui.theme.LocalStatusColors

/** Above this many pods, segments get too thin: a continuous bar instead. */
private const val MAX_SEGMENTS = 24

private val BarShape = RoundedCornerShape(50)

/**
 * A rollout's progress: one segment per desired pod, green once a new pod is ready, pulsing
 * while a new one starts (red when the rollout stalled), the track for the rest.
 */
@Composable
fun RolloutProgressBar(desired: Int, newReady: Int, newStarting: Int, done: Boolean, failed: Boolean, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val track = MaterialTheme.colorScheme.surfaceVariant
    val pulse by rememberInfiniteTransition(label = "rollout pulse").animateFloat(
        initialValue = 0.35f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(700, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "pulse",
    )
    val startingColor = (if (failed) colors.bad else colors.warn).copy(alpha = pulse)
    val total = desired.coerceAtLeast(1)
    val ready = if (done) total else newReady.coerceIn(0, total)
    val starting = if (done) 0 else newStarting.coerceIn(0, total - ready)

    if (total > MAX_SEGMENTS) {
        ContinuousBar(ready.toFloat() / total, (ready + starting).toFloat() / total, colors.ok, startingColor, track, modifier)
        return
    }
    Row(modifier.fillMaxWidth().height(10.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        repeat(total) { i ->
            val target = when {
                i < ready -> colors.ok
                i < ready + starting -> startingColor
                else -> track
            }
            // Pulsing segments follow the pulse directly; the others ease into their new color.
            val eased by animateColorAsState(target, tween(450), label = "segment")
            Box(Modifier.weight(1f).fillMaxHeight().clip(BarShape).background(if (i in ready until ready + starting) target else eased))
        }
    }
}

@Composable
private fun ContinuousBar(ready: Float, reached: Float, ok: Color, starting: Color, track: Color, modifier: Modifier) {
    val readyFill by animateFloatAsState(ready, tween(600, easing = FastOutSlowInEasing), label = "ready")
    val reachedFill by animateFloatAsState(reached, tween(600, easing = FastOutSlowInEasing), label = "reached")
    Box(modifier.fillMaxWidth().height(10.dp).clip(BarShape).background(track)) {
        Box(Modifier.fillMaxWidth(reachedFill).fillMaxHeight().clip(BarShape).background(starting))
        Box(Modifier.fillMaxWidth(readyFill).fillMaxHeight().clip(BarShape).background(Brush.horizontalGradient(listOf(ok.copy(alpha = 0.7f), ok))))
    }
}
