package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.theme.StatusColors

/** At or above this a bar is bad, whatever its warning threshold. */
private const val BAD_AT = 0.9f

/** The colour of a bar [fraction] full: ok below [warnAt], warn from it, bad from 90 %. */
fun usageColor(fraction: Float, warnAt: Float, colors: StatusColors): Color = when {
    fraction >= BAD_AT -> colors.bad
    fraction >= warnAt -> colors.warn
    else -> colors.ok
}

/**
 * Usage bar coloured by how full it is: the one bar style. An indeterminate "busy" bar is a
 * plain [LinearProgressIndicator].
 */
@Composable
fun UsageBar(fraction: Float, modifier: Modifier = Modifier, warnAt: Float = 0.75f) {
    LinearProgressIndicator(
        progress = { fraction },
        color = usageColor(fraction, warnAt, LocalStatusColors.current),
        trackColor = MaterialTheme.colorScheme.surfaceVariant,
        modifier = modifier.fillMaxWidth().height(6.dp),
        drawStopIndicator = {},
    )
}

/** A labelled bar: [label] on a quarter of the width, the bar, then [value] ("62%"). */
@Composable
fun MeterRow(label: String, fraction: Float, value: String, modifier: Modifier = Modifier, warnAt: Float = 0.75f) {
    Row(modifier, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        MutedText(label, modifier = Modifier.weight(0.25f), maxLines = 1)
        UsageBar(fraction, Modifier.weight(0.6f), warnAt = warnAt)
        Text(
            value,
            style = MaterialTheme.typography.bodySmall.copy(fontFeatureSettings = "tnum"),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            modifier = Modifier.weight(0.15f),
        )
    }
}
