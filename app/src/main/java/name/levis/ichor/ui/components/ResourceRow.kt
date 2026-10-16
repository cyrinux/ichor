package name.levis.ichor.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.model.ServiceHealth

/**
 * One resource in a list: its [health] dot (a blank of the same width when it has none), a
 * one-line [title], a [subtitle] in the health's colour only when it needs attention, then
 * [trailing] (a [Tag], a count, an overflow button) and a chevron when it opens something
 * ([onClick]) or expands ([expanded]). [content] goes under the head, aligned with the title.
 */
@Composable
fun ResourceRow(
    health: ServiceHealth?,
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    mono: Boolean = true,
    trailing: @Composable RowScope.() -> Unit = {},
    onClick: (() -> Unit)? = null,
    expanded: Boolean? = null,
    content: @Composable ColumnScope.() -> Unit = {},
) {
    val clickable = if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier
    Column(modifier.fillMaxWidth().then(clickable).defaultMinSize(minHeight = 48.dp).padding(vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (health != null) HealthDot(health) else Spacer(Modifier.size(10.dp))
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    title,
                    style = MaterialTheme.typography.bodyMedium,
                    fontFamily = if (mono) FontFamily.Monospace else null,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (subtitle != null) {
                    Text(
                        subtitle,
                        style = MaterialTheme.typography.labelSmall,
                        color = health?.takeIf { it.needsAttention }?.color() ?: MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            trailing()
            val chevron = when (expanded) {
                true -> Icons.Outlined.ExpandLess
                false -> Icons.Outlined.ExpandMore
                null -> if (onClick != null) Icons.AutoMirrored.Outlined.KeyboardArrowRight else null
            }
            if (chevron != null) {
                Icon(chevron, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 4.dp))
            }
        }
        Column(Modifier.padding(start = 22.dp)) { content() }
    }
}
