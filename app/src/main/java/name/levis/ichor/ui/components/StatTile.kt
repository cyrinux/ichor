package name.levis.ichor.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

/**
 * A number with its label ("3/3", "Nodes"), an optional icon before it and [meter] under it.
 * With [iconTile], the icon sits on a small secondary-container tile (the cluster's quieter
 * hue): for identity icons only, never for a status.
 */
@Composable
fun StatTile(
    icon: ImageVector?,
    value: String,
    label: String,
    modifier: Modifier = Modifier,
    iconTile: Boolean = false,
    meter: (@Composable () -> Unit)? = null,
) {
    Column(modifier) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (icon != null && iconTile) {
                Box(
                    Modifier
                        .padding(end = 8.dp)
                        .size(32.dp)
                        .clip(MaterialTheme.shapes.small)
                        .background(MaterialTheme.colorScheme.secondaryContainer),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSecondaryContainer, modifier = Modifier.size(20.dp))
                }
            } else if (icon != null) {
                Icon(
                    icon,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.padding(end = 8.dp).size(20.dp),
                )
            }
            // Narrow phones and long translations wrap rather than cut the label short.
            Column {
                Text(value, style = MaterialTheme.typography.titleMedium)
                Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        meter?.invoke()
    }
}

/** Two or three tiles side by side, each as wide as the others; each gets its weighted [Modifier]. */
@Composable
fun StatTileRow(modifier: Modifier = Modifier, vararg tiles: @Composable (Modifier) -> Unit) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        tiles.forEach { tile -> tile(Modifier.weight(1f)) }
    }
}
