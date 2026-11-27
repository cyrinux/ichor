package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfoV2
import androidx.compose.material3.adaptive.layout.AnimatedPane
import androidx.compose.material3.adaptive.layout.ListDetailPaneScaffold
import androidx.compose.material3.adaptive.layout.PaneAdaptedValue
import androidx.compose.material3.adaptive.layout.calculatePaneScaffoldDirective
import androidx.compose.material3.adaptive.layout.ThreePaneScaffoldValue
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

/**
 * The narrowest window, in dp, that shows a list and its detail side by side: Material's
 * "expanded" width class, i.e. tablets and unfolded foldables. A phone in landscape (at most
 * 840 dp wide) and a folded foldable (about 600 dp) stay single-pane on purpose: half of such a
 * window is too narrow for a node's or an app's screen.
 */
const val TwoPanesMinWidthDp = 840

/** The widest the two-column overview gets on a large tablet, centred like [ReadableWidth]. */
const val WideReadableWidthDp = 1200

/** Whether a window [widthDp] wide shows a list and its detail side by side. */
fun twoPanes(widthDp: Int): Boolean = widthDp >= TwoPanesMinWidthDp

/**
 * [twoPanes] for the current window, recomposed when it is resized, folded or unfolded. The
 * width class's lower bound is 0, 600, 840, 1200 or 1600 dp, so this is true from the
 * expanded class on.
 */
@Composable
fun rememberTwoPanes(): Boolean = twoPanes(currentWindowAdaptiveInfoV2().windowSizeClass.minWidthDp)

/**
 * A tabbed screen's content when one of its tabs is a [ListWithDetailPane]: with [twoPanes], as
 * wide as the overview's two columns, so the list and its detail both fit; otherwise
 * [pageContent]. The other tabs keep their [ReadableWidth] through [ReadableTab].
 */
fun Modifier.listDetailPageContent(padding: PaddingValues, twoPanes: Boolean): Modifier =
    if (twoPanes) padding(padding).readableWidth(WideReadableWidthDp.dp) else pageContent(padding)

/** A tab beside a [ListWithDetailPane] tab: at [ReadableWidth] when [twoPanes] widened the page. */
@Composable
fun ReadableTab(twoPanes: Boolean, content: @Composable () -> Unit) {
    if (!twoPanes) {
        content()
        return
    }
    Box(Modifier.readableWidth()) { content() }
}

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

/**
 * The item of [items] whose [id] is [picked], so the detail follows the list's refreshes. Null
 * when nothing is picked, the list is not read yet, or the item left it: the detail pane then
 * shows its placeholder again.
 */
fun <T> pickedById(items: List<T>?, picked: String?, id: (T) -> String): T? =
    picked?.let { wanted -> items?.firstOrNull { id(it) == wanted } }

/**
 * A list with [selected]'s detail beside it, for a list that is one tab of its screen (the
 * screen's top bar and tabs stay above both). Before a pick, or once the pick left the list,
 * the detail pane shows [emptyIcon] and [emptyText]. Back clears the pick ([onClear]), then
 * leaves as usual. Only for [rememberTwoPanes]: on a phone the list stays alone.
 */
@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
fun <T : Any> ListWithDetailPane(
    selected: T?,
    onClear: () -> Unit,
    emptyIcon: ImageVector,
    emptyText: String,
    list: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    detail: @Composable (T) -> Unit,
) {
    // Composed before the panes, so the detail's sheets and dialogs win over it.
    val back = rememberPredictiveBack(enabled = selected != null, onBack = onClear)
    ListDetailPaneScaffold(
        directive = calculatePaneScaffoldDirective(currentWindowAdaptiveInfoV2()),
        value = ListAndDetailExpanded,
        modifier = modifier,
        listPane = { AnimatedPane { list() } },
        detailPane = {
            AnimatedPane(modifier = Modifier.predictiveBackPreview(back)) {
                if (selected == null) EmptyDetailPane(emptyIcon, emptyText) else detail(selected)
            }
        },
    )
}
