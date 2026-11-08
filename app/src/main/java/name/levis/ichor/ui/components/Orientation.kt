package name.levis.ichor.ui.components

import android.app.Activity
import android.content.pm.ActivityInfo
import androidx.activity.compose.LocalActivity
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * The smallest window width, in dp, from which every screen rotates: tablets, unfolded
 * foldables and desktop windows. Below it (a phone), the app holds portrait and only the
 * wide-content screens ([AllowLandscape]) rotate. Android 16 ignores orientation requests on
 * large screens anyway, and Play flags apps that lock them.
 */
const val FreeRotationMinWidthDp = 600

/** Whether a window whose smallest side is [smallestWidthDp] holds portrait outside the wide screens. */
fun locksPortrait(smallestWidthDp: Int): Boolean = smallestWidthDp < FreeRotationMinWidthDp

/**
 * The orientation the activity asks for when it is created. A phone holds portrait, except when
 * the activity is recreated ([restoring], e.g. by a rotation) while a wide screen had unlocked it
 * ([current] is full user): that screen is still on top and keeps its landscape. Larger windows
 * ask for nothing.
 */
fun startOrientation(smallestWidthDp: Int, current: Int, restoring: Boolean): Int = when {
    !locksPortrait(smallestWidthDp) -> ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
    restoring && current == ActivityInfo.SCREEN_ORIENTATION_FULL_USER -> current
    else -> ActivityInfo.SCREEN_ORIENTATION_USER_PORTRAIT
}

/**
 * Lets the screen that calls it rotate on a phone while it is on screen (logs, shells, charts,
 * YAML, diffs, maps); leaving it returns to portrait. Call it at the screen's root, never inside
 * a sheet or a dialog (a rotation recreates the activity and would close them). Does nothing on
 * tablets and foldables, which always rotate, nor in previews.
 */
@Composable
fun AllowLandscape() {
    val activity = LocalActivity.current ?: return
    val locks = locksPortrait(LocalConfiguration.current.smallestScreenWidthDp)
    DisposableEffect(activity, locks) {
        if (locks) LandscapeHolds.acquire(activity)
        onDispose { if (locks) LandscapeHolds.release(activity) }
    }
}

/**
 * A chart's height in a window [widthDp] × [heightDp]: [minDp] in portrait, and about 45 % of
 * the window's height in landscape, where the chart is what the screen was turned for.
 */
fun chartHeightDp(minDp: Int, widthDp: Int, heightDp: Int): Int =
    if (widthDp > heightDp) maxOf(minDp, heightDp * 45 / 100) else minDp

/** [chartHeightDp] for the current window, at least [min] high. */
@Composable
fun rememberChartHeight(min: Dp): Dp {
    val config = LocalConfiguration.current
    return chartHeightDp(min.value.toInt(), config.screenWidthDp, config.screenHeightDp).dp
}

/**
 * The wide screens currently holding the rotation unlocked, so nested or overlapping ones (a
 * screen entering before the previous one left) do not fight: portrait comes back with the last.
 */
private object LandscapeHolds {
    private var count = 0

    fun acquire(activity: Activity) {
        count += 1
        activity.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_FULL_USER
    }

    fun release(activity: Activity) {
        count = (count - 1).coerceAtLeast(0)
        // A rotation disposes the old activity's screens: the recreated one keeps its landscape.
        if (count == 0 && !activity.isChangingConfigurations) {
            activity.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_USER_PORTRAIT
        }
    }
}
