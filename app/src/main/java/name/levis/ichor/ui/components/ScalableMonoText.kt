package name.levis.ichor.ui.components

import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.TextUnit
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.MonoTextScale
import name.levis.ichor.ui.theme.monoSmall

/** The screens whose dense monospace text the user resizes; each keeps its own size. */
enum class MonoScreen(val key: String) { LOGS("logs"), CAPTURE("capture") }

/** What a [ScalableMonoText] hands its rows: the picked style and the screen-reader actions. */
private class MonoText(val style: TextStyle, val actions: List<CustomAccessibilityAction>)

private val LocalMonoText = staticCompositionLocalOf<MonoText?> { null }

/** The dense monospace style: the size the user picked for this screen, else the theme's. */
@Composable
fun monoTextStyle(): TextStyle = LocalMonoText.current?.style ?: MaterialTheme.typography.monoSmall

/**
 * "Larger text" / "Smaller text" on a row, for screen-reader users who cannot pinch. Rows carry
 * them because TalkBack focuses rows, not the list. No-op outside a [ScalableMonoText].
 */
@Composable
fun Modifier.monoTextActions(): Modifier {
    val actions = LocalMonoText.current?.actions ?: return this
    return this.semantics { customActions = actions }
}

/**
 * Lets the user resize [content]'s monospace text ([monoTextStyle]): pinch with two fingers
 * (one finger still scrolls), or the rows' [monoTextActions]. Remembered per [screen].
 */
@Composable
fun ScalableMonoText(screen: MonoScreen, modifier: Modifier = Modifier, content: @Composable BoxScope.() -> Unit) {
    val prefs = (LocalContext.current.applicationContext as TalosApp).uiPreferences
    val base = MaterialTheme.typography.monoSmall
    val baseSp = base.fontSize.value
    var factor by remember(screen) { mutableFloatStateOf(MonoTextScale.clamp(prefs.monoTextScale(screen.key), baseSp)) }
    val save = { next: Float ->
        factor = next
        prefs.setMonoTextScale(screen.key, next)
    }
    val larger = stringResource(R.string.mono_text_larger)
    val smaller = stringResource(R.string.mono_text_smaller)
    val actions = remember(larger, smaller, baseSp) {
        listOf(
            CustomAccessibilityAction(larger) { save(MonoTextScale.larger(factor, baseSp)); true },
            CustomAccessibilityAction(smaller) { save(MonoTextScale.smaller(factor, baseSp)); true },
        )
    }
    CompositionLocalProvider(LocalMonoText provides MonoText(base.scaled(factor), actions)) {
        Box(
            modifier.pinchToZoom(
                onZoom = { zoom -> factor = MonoTextScale.clamp(factor * zoom, baseSp) },
                onEnd = { save(factor) },
            ),
        ) { content() }
    }
}

private fun TextStyle.scaled(factor: Float): TextStyle =
    if (factor == 1f) this else copy(fontSize = fontSize.scaledBy(factor), lineHeight = lineHeight.scaledBy(factor))

private fun TextUnit.scaledBy(factor: Float): TextUnit = if (this == TextUnit.Unspecified) this else this * factor

/**
 * Two-finger zoom that leaves one-finger drags to the list below: events are read before the
 * list sees them, and consumed only while two or more fingers are down.
 */
private fun Modifier.pinchToZoom(onZoom: (Float) -> Unit, onEnd: () -> Unit): Modifier = this.then(
    Modifier.pointerInput(Unit) {
        awaitEachGesture {
            awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
            var zoomed = false
            do {
                val event = awaitPointerEvent(PointerEventPass.Initial)
                if (event.changes.count { it.pressed } >= 2) {
                    val zoom = event.calculateZoom()
                    if (zoom != 1f) {
                        onZoom(zoom)
                        zoomed = true
                    }
                    event.changes.forEach { it.consume() }
                }
            } while (event.changes.any { it.pressed })
            if (zoomed) onEnd()
        }
    },
)
