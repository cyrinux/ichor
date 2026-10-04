package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Sensors
import androidx.compose.material.icons.outlined.SensorsOff
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.material.icons.outlined.Info
import name.levis.ichor.R
import name.levis.ichor.model.versionNotice
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * State of a live stream: "Live" while it runs, the error it ended with, or "Stopped".
 * Icon plus text, so it does not rely on colour.
 */
@Composable
fun LiveIndicator(streaming: Boolean, error: String?, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    // "This Talos version cannot stream that" is information, not a failure.
    val notice = error?.let(::versionNotice)
    val (icon, color, text) = when {
        streaming -> Triple(Icons.Outlined.Sensors, colors.ok, stringResource(R.string.common_live))
        notice != null -> Triple(Icons.Outlined.Info, colors.muted, notice.text())
        error != null -> Triple(Icons.Outlined.ErrorOutline, colors.bad, stringResource(R.string.common_stream_failed, error))
        else -> Triple(Icons.Outlined.SensorsOff, colors.muted, stringResource(R.string.common_stream_stopped))
    }
    // Polite: a stream starting, failing or stopping is announced without cutting speech off.
    Row(modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite }, verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(16.dp))
        Text(text, color = color, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(start = 6.dp))
    }
}
