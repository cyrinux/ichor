package name.levis.ichor.ui.nav

import androidx.navigation.NavHostController

/**
 * The app's navigation, for the screenshot walk (androidTest DemoScreenshots) to open a screen
 * directly. Only debug builds set it (Navigation.kt).
 */
object TestRoute {
    var nav: NavHostController? = null
}
