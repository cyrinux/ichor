package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp

/**
 * A card with the overview cards' header: an optional [leading] icon, [title], a muted
 * [subtitle], one [pill] at the end and a chevron when the card opens a screen ([onOpen]);
 * [content] below. The title is a heading for screen readers.
 */
@Composable
fun SectionCard(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    pill: (@Composable () -> Unit)? = null,
    onOpen: (() -> Unit)? = null,
    leading: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val body: @Composable ColumnScope.() -> Unit = {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            SectionHeader(
                title,
                subtitle = subtitle,
                leading = leading,
                pill = pill,
                chevron = if (onOpen != null) Icons.AutoMirrored.Outlined.KeyboardArrowRight else null,
            )
            content()
        }
    }
    if (onOpen != null) {
        Card(onClick = onOpen, modifier = modifier.fillMaxWidth(), content = body)
    } else {
        Card(modifier = modifier.fillMaxWidth(), content = body)
    }
}

/**
 * The header of a [SectionCard], for cards that frame themselves (one that expands in place):
 * [leading], [title] over a muted [subtitle], one [pill], then [chevron] (opens or expands)
 * and [trailing] before it.
 */
@Composable
fun SectionHeader(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    leading: (@Composable () -> Unit)? = null,
    pill: (@Composable () -> Unit)? = null,
    chevron: ImageVector? = null,
    trailing: (@Composable () -> Unit)? = null,
) {
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        if (leading != null) {
            leading()
            Spacer(Modifier.size(12.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium, modifier = Modifier.semantics { heading() })
            if (subtitle != null) MutedText(subtitle)
        }
        trailing?.invoke()
        pill?.invoke()
        if (chevron != null) {
            Icon(
                chevron,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 4.dp),
            )
        }
    }
}
