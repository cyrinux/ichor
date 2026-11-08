package name.levis.ichor.ui.tour

import androidx.annotation.StringRes
import androidx.compose.ui.geometry.Rect
import name.levis.ichor.R

/** What a coach mark of the demo tour points at on the Talos demo home. */
enum class TourAnchor { Switcher, Nodes, DegradedNode, Actions, Settings }

/** One coach mark: the part of the home it points at and what it says. */
data class TourStep(val anchor: TourAnchor, @StringRes val title: Int, @StringRes val text: Int)

/** The demo tour, in order. */
val demoTourSteps = listOf(
    TourStep(TourAnchor.Switcher, R.string.tour_switcher_title, R.string.tour_switcher_text),
    TourStep(TourAnchor.Nodes, R.string.tour_nodes_title, R.string.tour_nodes_text),
    TourStep(TourAnchor.DegradedNode, R.string.tour_degraded_title, R.string.tour_degraded_text),
    TourStep(TourAnchor.Actions, R.string.tour_actions_title, R.string.tour_actions_text),
    TourStep(TourAnchor.Settings, R.string.tour_settings_title, R.string.tour_settings_text),
)

/** The step shown ([step], its index in the steps) as the [number]-th of the [total] on screen. */
data class TourPosition(val step: Int, val number: Int, val total: Int) {
    val last: Boolean get() = number == total
}

/**
 * The first of [steps] from index [from] whose anchor is [present], numbered among the present
 * ones; null when none is left (the tour is over, or nothing it points at is on screen yet).
 */
fun tourPosition(steps: List<TourStep>, from: Int, present: Set<TourAnchor>): TourPosition? {
    val shown = steps.indices.filter { steps[it].anchor in present }
    val at = shown.indexOfFirst { it >= from }
    if (at < 0) return null
    return TourPosition(step = shown[at], number = at + 1, total = shown.size)
}

/** The anchors laid out with some of their area inside [viewport]: a card scrolled away is not. */
fun presentAnchors(bounds: Map<TourAnchor, Rect>, viewport: Rect): Set<TourAnchor> =
    bounds.filterValues { !it.isEmpty && it.overlaps(viewport) }.keys
