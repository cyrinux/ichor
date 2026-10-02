package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.ui.UiState
import java.text.DateFormat
import java.util.Date

/**
 * Screen footer saying how fresh the data is: "Updated 15:42:10 · 2 min ago", or, while a
 * refresh runs over cached data, "Refreshing… (showing data from 15:40:02)". Nothing until loaded.
 */
@Composable
fun DataFreshness(state: UiState<*>, modifier: Modifier = Modifier, edgeToEdge: Boolean = true) {
    val loaded = state as? UiState.Loaded<*> ?: return
    // Re-render every 15 s so "x min ago" stays true.
    val now by produceState(System.currentTimeMillis()) {
        while (true) {
            delay(15_000)
            value = System.currentTimeMillis()
        }
    }
    val time = DateFormat.getTimeInstance(DateFormat.MEDIUM).format(Date(loaded.fetchedAt))
    val text = if (loaded.refreshing) {
        stringResource(R.string.common_refreshing_showing, time)
    } else {
        stringResource(R.string.common_updated_ago, time, agoText(age(now - loaded.fetchedAt)))
    }
    Surface(color = MaterialTheme.colorScheme.surfaceContainer, modifier = modifier.fillMaxWidth()) {
        Text(
            text,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = (if (edgeToEdge) Modifier.navigationBarsPadding() else Modifier)
                .padding(horizontal = 16.dp, vertical = 6.dp),
        )
    }
}

/** How long ago, in the largest whole unit; formatted with plurals by [agoText]. */
internal data class Age(val unit: AgeUnit, val count: Int)

internal enum class AgeUnit { JUST_NOW, MINUTES, HOURS, DAYS }

internal fun age(millis: Long): Age {
    val seconds = millis.coerceAtLeast(0) / 1000
    return when {
        seconds < 60 -> Age(AgeUnit.JUST_NOW, 0)
        seconds < 3_600 -> Age(AgeUnit.MINUTES, (seconds / 60).toInt())
        seconds < 86_400 -> Age(AgeUnit.HOURS, (seconds / 3_600).toInt())
        else -> Age(AgeUnit.DAYS, (seconds / 86_400).toInt())
    }
}

@Composable
private fun agoText(age: Age): String = when (age.unit) {
    AgeUnit.JUST_NOW -> stringResource(R.string.common_just_now)
    AgeUnit.MINUTES -> pluralStringResource(R.plurals.common_minutes_ago, age.count, age.count)
    AgeUnit.HOURS -> pluralStringResource(R.plurals.common_hours_ago, age.count, age.count)
    AgeUnit.DAYS -> pluralStringResource(R.plurals.common_days_ago, age.count, age.count)
}

/** "just now", "5 min ago", … for something that happened [millis] ago. */
@Composable
fun agoLabel(millis: Long): String = agoText(age(millis))
