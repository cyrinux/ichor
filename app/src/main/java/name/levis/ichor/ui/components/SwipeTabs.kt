package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import kotlin.math.abs

/** The pages of a screen with two tabs. */
val TWO_TABS = listOf(0, 1)

/**
 * The content under a tab row, swiped sideways from one tab to the next. [selected] stays the
 * screen's own (saveable) tab state: tapping a tab slides the pager there, and a swipe that
 * settles on a page selects its tab. Gestures inside a page (row swipes, chart scrubbing,
 * horizontal scrolling up to its edge) come first, and only a neighbour being dragged in is
 * composed, so tabs that stream don't all run at once. [selected] must be one of [tabs].
 */
@Composable
fun <T : Any> SwipeTabPager(
    tabs: List<T>,
    selected: T,
    onSelect: (T) -> Unit,
    modifier: Modifier = Modifier,
    page: @Composable (T) -> Unit,
) {
    val index = tabs.indexOf(selected).coerceAtLeast(0)
    val pager = rememberPagerState(initialPage = index) { tabs.size }
    val currentTabs by rememberUpdatedState(tabs)
    val currentSelected by rememberUpdatedState(selected)
    val currentOnSelect by rememberUpdatedState(onSelect)

    LaunchedEffect(index, tabs.size) {
        // Also when a tap interrupted the slide short of the page it was on.
        if (pager.currentPage == index && pager.currentPageOffsetFraction == 0f) return@LaunchedEffect
        // Sliding further would compose (and start) every tab in between.
        if (abs(pager.currentPage - index) > 1) pager.scrollToPage(index) else pager.animateScrollToPage(index)
    }
    LaunchedEffect(pager) {
        snapshotFlow { pager.settledPage }.collect { settled ->
            if (pager.targetPage != settled || currentSelected !in currentTabs) return@collect
            currentTabs.getOrNull(settled)?.let { if (it != currentSelected) currentOnSelect(it) }
        }
    }

    HorizontalPager(
        state = pager,
        modifier = modifier.fillMaxSize(),
        key = { tabs[it] },
        verticalAlignment = Alignment.Top,
    ) { page(tabs[it]) }
}
