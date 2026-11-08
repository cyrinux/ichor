package name.levis.ichor.ui.design

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.calculateEndPadding
import androidx.compose.foundation.layout.calculateStartPadding
import androidx.compose.material3.FabPosition
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ScaffoldDefaults
import androidx.compose.material3.contentColorFor
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.unit.dp
import name.levis.ichor.data.UiStyle

/** What the floating toolbar covers, left free at the bottom of the content so lists scroll past it. */
private val FloatingActionsHeight = 88.dp

@Composable
fun IchorScaffold(
    modifier: Modifier = Modifier,
    topBar: @Composable () -> Unit = {},
    bottomBar: @Composable () -> Unit = {},
    snackbarHost: @Composable () -> Unit = {},
    floatingActionButton: @Composable () -> Unit = {},
    floatingActionButtonPosition: FabPosition = FabPosition.End,
    containerColor: Color = MaterialTheme.colorScheme.background,
    contentColor: Color = contentColorFor(containerColor),
    contentWindowInsets: WindowInsets = ScaffoldDefaults.contentWindowInsets,
    content: @Composable (PaddingValues) -> Unit,
) {
    if (LocalUiStyle.current != UiStyle.EXPRESSIVE) {
        Scaffold(
            modifier,
            topBar,
            bottomBar,
            snackbarHost,
            floatingActionButton,
            floatingActionButtonPosition,
            containerColor,
            contentColor,
            contentWindowInsets,
            content,
        )
        return
    }
    val floating = remember { FloatingActions() }
    val actions = floating.content
    val direction = LocalLayoutDirection.current
    CompositionLocalProvider(LocalFloatingActions provides floating) {
        Scaffold(
            modifier,
            topBar,
            bottomBar,
            snackbarHost,
            floatingActionButton = {
                if (actions == null) {
                    floatingActionButton()
                } else {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        FloatingActionsToolbar(actions)
                        floatingActionButton()
                    }
                }
            },
            floatingActionButtonPosition = if (actions == null) floatingActionButtonPosition else FabPosition.Center,
            containerColor,
            contentColor,
            contentWindowInsets,
        ) { padding ->
            content(
                if (actions == null) {
                    padding
                } else {
                    PaddingValues(
                        start = padding.calculateStartPadding(direction),
                        top = padding.calculateTopPadding(),
                        end = padding.calculateEndPadding(direction),
                        bottom = padding.calculateBottomPadding() + FloatingActionsHeight,
                    )
                },
            )
        }
    }
}
