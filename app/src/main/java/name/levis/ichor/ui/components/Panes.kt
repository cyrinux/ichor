package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfoV2
import androidx.compose.material3.adaptive.layout.PaneAdaptedValue
import androidx.compose.material3.adaptive.layout.ThreePaneScaffoldValue
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector

/**
 * The narrowest window, in dp, that shows a list and its detail side by side: Material's
 * "expanded" width class, i.e. tablets and unfolded foldables. A phone in landscape (at most
 * 840 dp wide) and a folded foldable (about 600 dp) stay single-pane on purpose: half of such a
 * window is too narrow for a node's or an app's screen.
 */
const val TwoPanesMinWidthDp = 840

/** Whether a window [widthDp] wide shows a list and its detail side by side. */
fun twoPanes(widthDp: Int): Boolean = widthDp >= TwoPanesMinWidthDp

/**
 * [twoPanes] for the current window, recomposed when it is resized, folded or unfolded. The
 * width class's lower bound is 0, 600, 840, 1200 or 1600 dp, so this is true from the
 * expanded class on.
 */
@Composable
fun rememberTwoPanes(): Boolean = twoPanes(currentWindowAdaptiveInfoV2().windowSizeClass.minWidthDp)

/** List and detail panes both shown, nothing in the extra pane. */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
val ListAndDetailExpanded = ThreePaneScaffoldValue(
    primary = PaneAdaptedValue.Expanded,
    secondary = PaneAdaptedValue.Expanded,
    tertiary = PaneAdaptedValue.Hidden,
)

/** The detail pane before anything is picked in the list: [icon] and [text], centred. */
@Composable
fun EmptyDetailPane(icon: ImageVector, text: String, modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        EmptyState(icon = icon, text = text)
    }
}
