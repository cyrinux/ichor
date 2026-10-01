package name.levis.talosmobile.ui.components

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
import kotlinx.coroutines.delay
import name.levis.talosmobile.ui.UiState
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
        "Refreshing… (showing data from $time)"
    } else {
        "Updated $time · ${ago(now - loaded.fetchedAt)}"
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

internal fun ago(millis: Long): String {
    val seconds = millis.coerceAtLeast(0) / 1000
    return when {
        seconds < 60 -> "just now"
        seconds < 3_600 -> "${seconds / 60} min ago"
        seconds < 86_400 -> "${seconds / 3_600} h ago"
        else -> "${seconds / 86_400} d ago"
    }
}
