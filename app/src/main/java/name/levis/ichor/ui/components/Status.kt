/**
 * How a screen shows state, so every screen reads as one system:
 *
 * - Health colours come only from [LocalStatusColors] (ok / warn / bad / muted), never from the
 *   cluster's hue.
 * - [HealthDot] leads a list row; [StatusPill] is one per header or card, for a named state;
 *   [Tag] is for categories (role, type, scope, QoS, priority class) and carries a health colour
 *   only when the value itself is a problem.
 * - A tag whose value is the same on every row of a list is dropped from the rows: show it once
 *   in the header, or only on the exceptions.
 * - Numbers are [StatTile]s, never prose; usage is [UsageBar] or [MeterRow], with the caller's
 *   thresholds.
 * - An explanatory paragraph is an [InfoHint]; at most one filled button per screen, row-level
 *   mutations live in the row's overflow menu or sheet.
 * - Errors are [ErrorBox] (the whole screen) or [InlineError] (a section), never a plain text.
 */
package name.levis.ichor.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.theme.LocalStatusColors

@Composable
fun ServiceHealth.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        ServiceHealth.CRITICAL -> colors.bad
        ServiceHealth.WARNING -> colors.warn
        ServiceHealth.OK -> colors.ok
        ServiceHealth.IDLE, ServiceHealth.UNKNOWN -> colors.muted
    }
}

@Composable
fun ServiceHealth.label(): String = stringResource(
    when (this) {
        ServiceHealth.CRITICAL -> R.string.data_services_health_critical
        ServiceHealth.WARNING -> R.string.data_services_health_warning
        ServiceHealth.OK -> R.string.data_services_health_ok
        ServiceHealth.IDLE -> R.string.data_services_health_idle
        ServiceHealth.UNKNOWN -> R.string.data_services_health_unknown
    },
)

/** A row's health, read out by TalkBack. */
@Composable
fun HealthDot(health: ServiceHealth, modifier: Modifier = Modifier) {
    val label = health.label()
    Box(modifier.size(10.dp).background(health.color(), CircleShape).semantics { contentDescription = label })
}

/** A header's or card's named health: its label in its colour. */
@Composable
fun StatusPill(health: ServiceHealth, modifier: Modifier = Modifier) = StatusPill(health.label(), health.color(), modifier)

/** A small tinted label for a category: a policy kind, a direction, a type, a QoS class. */
@Composable
fun Tag(label: String, color: Color = LocalStatusColors.current.muted, modifier: Modifier = Modifier, mono: Boolean = false) {
    Surface(modifier, shape = RoundedCornerShape(6.dp), color = color.copy(alpha = 0.14f), contentColor = color) {
        Text(
            label,
            style = MaterialTheme.typography.labelSmall,
            fontFamily = if (mono) FontFamily.Monospace else null,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
        )
    }
}
