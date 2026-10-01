package name.levis.ichor.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.util.DurationFormat
import name.levis.ichor.util.formatDuration

/** Duration patterns in the current language. */
@Composable
fun durationFormat(): DurationFormat {
    // Resource patterns are read raw (no args) and formatted by formatDuration.
    return DurationFormat(
        daysHours = stringResource(R.string.common_duration_days_hours),
        hoursMinutes = stringResource(R.string.common_duration_hours_minutes),
        minutes = stringResource(R.string.common_duration_minutes),
        lessThanMinute = stringResource(R.string.common_duration_less_than_minute),
    )
}

@Composable
fun localizedDuration(seconds: Long): String = formatDuration(seconds, durationFormat())
