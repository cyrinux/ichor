package name.levis.ichor.ui.components

import android.provider.Settings
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.R

/** What the content being loaded will look like. */
enum class SkeletonStyle {
    /** A list of cards or rows: most screens. */
    CARDS,

    /** Lines of text: logs, YAML. */
    TEXT,
}

/**
 * Grey shapes of the content to come, pulsing, in place of a blank page with a spinner.
 * More shapes than any screen is tall are laid out and the overflow is clipped, so the
 * skeleton fills whatever room it is given without measuring it first.
 */
@Composable
fun SkeletonBox(modifier: Modifier = Modifier, style: SkeletonStyle = SkeletonStyle.CARDS) {
    val description = stringResource(R.string.common_loading)
    val pulse = skeletonPulse()
    Box(
        modifier
            .fillMaxSize()
            .clipToBounds()
            .clearAndSetSemantics {
                contentDescription = description
                progressBarRangeInfo = ProgressBarRangeInfo.Indeterminate
            },
    ) {
        Column(
            Modifier
                .wrapContentHeight(Alignment.Top, unbounded = true)
                .graphicsLayer { alpha = pulse?.value ?: 1f }
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(if (style == SkeletonStyle.TEXT) 10.dp else 12.dp),
        ) {
            when (style) {
                SkeletonStyle.CARDS -> repeat(SKELETON_CARDS) { SkeletonCard(it) }
                SkeletonStyle.TEXT -> repeat(SKELETON_LINES) { SkeletonBar(TEXT_WIDTHS[it % TEXT_WIDTHS.size], 10.dp) }
            }
        }
    }
}

/** A card with a title and two lines; the widths vary with [index] so the page does not look ruled. */
@Composable
private fun SkeletonCard(index: Int) {
    Column(
        Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceContainerLow, RoundedCornerShape(12.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        SkeletonBar(TITLE_WIDTHS[index % TITLE_WIDTHS.size], 14.dp)
        SkeletonBar(TEXT_WIDTHS[index % TEXT_WIDTHS.size], 10.dp)
        SkeletonBar(TEXT_WIDTHS[(index + 2) % TEXT_WIDTHS.size] * 0.7f, 10.dp)
    }
}

@Composable
private fun SkeletonBar(widthFraction: Float, height: Dp) {
    Box(
        Modifier
            .fillMaxWidth(widthFraction)
            .height(height)
            .background(MaterialTheme.colorScheme.surfaceContainerHighest, RoundedCornerShape(50)),
    )
}

/** The pulsing opacity; null (the shapes then stand still) with animations turned off in the system settings. */
@Composable
private fun skeletonPulse(): State<Float>? {
    val context = LocalContext.current
    val motion = remember(context) {
        Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) != 0f
    }
    if (!motion) return null
    return rememberInfiniteTransition(label = "skeleton").animateFloat(
        initialValue = 1f,
        targetValue = 0.45f,
        animationSpec = infiniteRepeatable(tween(PULSE_MILLIS, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "skeletonAlpha",
    )
}

private const val SKELETON_CARDS = 12
private const val SKELETON_LINES = 60
private const val PULSE_MILLIS = 900
private val TITLE_WIDTHS = listOf(0.45f, 0.6f, 0.35f, 0.5f)
private val TEXT_WIDTHS = listOf(0.9f, 0.7f, 0.95f, 0.55f, 0.8f, 0.4f, 0.85f)
