package name.levis.ichor.ui.nav

import kotlinx.coroutines.flow.MutableStateFlow

/**
 * A route to open, set by the screenshot walk (androidTest DemoScreenshots) to reach a screen
 * directly. Only debug builds listen (Navigation.kt).
 */
object TestRoute {
    val route = MutableStateFlow<String?>(null)
}
