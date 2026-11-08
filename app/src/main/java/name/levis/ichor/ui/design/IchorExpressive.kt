package name.levis.ichor.ui.design

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.ContainedLoadingIndicator
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.HorizontalFloatingToolbar
import androidx.compose.material3.MaterialExpressiveTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MotionScheme
import androidx.compose.material3.Shapes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import name.levis.ichor.data.UiStyle

/** Bolder corners: cards and chips read theirs from the theme, so they follow. */
private val ExpressiveShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(12.dp),
    medium = RoundedCornerShape(20.dp),
    large = RoundedCornerShape(28.dp),
    extraLarge = RoundedCornerShape(36.dp),
)

/** Material 3 Expressive: the same colours and type, with its springy motion and bolder shapes. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun ExpressiveTheme(colors: ColorScheme, content: @Composable () -> Unit) {
    MaterialExpressiveTheme(
        colorScheme = colors,
        motionScheme = MotionScheme.expressive(),
        shapes = ExpressiveShapes,
        typography = MaterialTheme.typography,
        content = content,
    )
}

/** A wait's indicator in the UI style on screen. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun IchorLoadingIndicator(modifier: Modifier = Modifier) {
    if (LocalUiStyle.current == UiStyle.EXPRESSIVE) {
        ContainedLoadingIndicator(modifier)
    } else {
        CircularProgressIndicator(modifier)
    }
}

/** A centred [IchorLoadingIndicator]. */
@Composable
fun IchorLoadingBox(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        IchorLoadingIndicator()
    }
}

/** Where a screen's actions float, when the style draws them apart from the top bar: see [IchorScaffold]. */
class FloatingActions {
    var content by mutableStateOf<(@Composable () -> Unit)?>(null)
}

/** The scaffold's [FloatingActions] in a style that floats them, null where they stay in the top bar. */
val LocalFloatingActions = staticCompositionLocalOf<FloatingActions?> { null }

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun FloatingActionsToolbar(content: @Composable () -> Unit) {
    HorizontalFloatingToolbar(expanded = true) { content() }
}
