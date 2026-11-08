package name.levis.ichor.ui.nav

import androidx.navigation.NavHostController
import name.levis.ichor.data.MaskEpoch

/**
 * The switch the back stack was last rebuilt after ([MaskEpoch] values), kept for the process
 * so a switch that ended while the navigation was not composed (app lock, background) is
 * applied when it is again: the reset follows the state, not the moment the switch ended.
 */
internal class MaskResetMark(private var resetAt: Int) {
    /** A switch ended (even [epoch]) since the last rebuild, and none is running. */
    fun due(epoch: Int): Boolean = epoch % 2 == 0 && epoch != resetAt

    fun done(epoch: Int) {
        resetAt = epoch
    }
}

/** The process' mark: what is on screen at launch was read under the setting of that moment. */
internal val navMaskMark = MaskResetMark(MaskEpoch.value.value)

/**
 * The back stack rebuilt once screenshot mode switched: [start] (the overview, or the import
 * screen without a config) and, when the switch was made there, Settings and the page shown
 * ([route] and its [page] argument). Every other screen goes: their view models, and the
 * names their routes carry (a node, a namespace), were read under the previous setting.
 */
internal fun maskResetStack(start: String, route: String?, page: String?): List<String> = when {
    route == Routes.SETTINGS_PAGE && page != null -> listOf(start, Routes.SETTINGS, "settings/$page")
    route == Routes.SETTINGS -> listOf(start, Routes.SETTINGS)
    else -> listOf(start)
}

/**
 * Drops the whole back stack, the start destination too (its view models hold the home read
 * under the previous setting), and opens [maskResetStack] again, the screen on top last.
 */
internal fun NavHostController.rebuildAfterMaskSwitch(start: String) {
    val entry = currentBackStackEntry
    val stack = maskResetStack(start, entry?.destination?.route, entry?.arguments?.getString("page"))
    stack.forEachIndexed { index, route ->
        navigate(route) {
            if (index == 0) popUpTo(graph.id) { inclusive = true }
        }
    }
}
