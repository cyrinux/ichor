package name.levis.ichor.ui.components

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Tab
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

/**
 * A [Tab] whose label takes the primary color only when selected. Material's own Tab keeps the
 * row's primary color for unselected tabs too, which reads as a meaningful highlight.
 * [unselectedContentColor] can dim a tab whose feature the node lacks.
 */
@Composable
fun AppTab(
    selected: Boolean,
    onClick: () -> Unit,
    text: @Composable () -> Unit,
    icon: (@Composable () -> Unit)? = null,
    unselectedContentColor: Color = MaterialTheme.colorScheme.onSurfaceVariant,
) {
    Tab(
        selected = selected,
        onClick = onClick,
        text = text,
        icon = icon,
        unselectedContentColor = unselectedContentColor,
    )
}
