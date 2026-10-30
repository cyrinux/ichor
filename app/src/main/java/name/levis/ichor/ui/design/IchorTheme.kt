package name.levis.ichor.ui.design

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import name.levis.ichor.data.ThemeMode
import name.levis.ichor.data.UiStyle
import name.levis.ichor.model.DEFAULT_CLUSTER_SEED
import name.levis.ichor.ui.theme.TalosTheme

/** The UI style on screen, read by the `Ichor*` wrappers of this package. */
val LocalUiStyle = staticCompositionLocalOf { UiStyle.MATERIAL3 }

/**
 * The app's theme: [mode] and [seed] decide the colours, [style] the design language that draws
 * the chrome. Screens call the `Ichor*` wrappers, so a new style changes this package only.
 */
@Composable
fun IchorTheme(
    mode: ThemeMode = ThemeMode.AUTO,
    style: UiStyle = UiStyle.MATERIAL3,
    seed: Int = DEFAULT_CLUSTER_SEED,
    content: @Composable () -> Unit,
) {
    CompositionLocalProvider(LocalUiStyle provides style) {
        TalosTheme(mode, seed, content)
    }
}
