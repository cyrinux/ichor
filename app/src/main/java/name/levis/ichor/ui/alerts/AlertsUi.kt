package name.levis.ichor.ui.alerts

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmSeverity
import name.levis.ichor.model.AmSilenceState
import name.levis.ichor.ui.theme.LocalStatusColors

/** Critical in the error colour, warning in the warn one, info in the primary, anything else muted. */
@Composable
fun severityColor(severity: String): Color {
    val colors = LocalStatusColors.current
    return when (severity) {
        AmSeverity.CRITICAL -> colors.bad
        AmSeverity.WARNING -> colors.warn
        AmSeverity.INFO -> MaterialTheme.colorScheme.primary
        else -> colors.muted
    }
}

@Composable
fun severityLabel(severity: String): String = when (severity) {
    AmSeverity.CRITICAL -> stringResource(R.string.alerts_severity_critical)
    AmSeverity.WARNING -> stringResource(R.string.alerts_severity_warning)
    AmSeverity.INFO -> stringResource(R.string.alerts_severity_info)
    else -> stringResource(R.string.alerts_severity_other)
}

/** "Firing", "Silenced", "Inhibited" or "Silenced, inhibited". */
@Composable
fun stateLabel(alert: AmAlert): String = when {
    alert.silenced && alert.inhibited -> stringResource(R.string.alerts_state_silenced_inhibited)
    alert.silenced -> stringResource(R.string.alerts_state_silenced)
    alert.inhibited -> stringResource(R.string.alerts_state_inhibited)
    else -> stringResource(R.string.alerts_state_firing)
}

@Composable
fun silenceStateLabel(state: String): String = when (state) {
    AmSilenceState.ACTIVE -> stringResource(R.string.alerts_silence_active)
    AmSilenceState.PENDING -> stringResource(R.string.alerts_silence_pending)
    else -> stringResource(R.string.alerts_silence_expired)
}

/** A severity's dot, beside an alert group's name. */
@Composable
fun SeverityDot(severity: String, modifier: Modifier = Modifier) {
    Box(modifier.size(10.dp).background(severityColor(severity), CircleShape))
}
