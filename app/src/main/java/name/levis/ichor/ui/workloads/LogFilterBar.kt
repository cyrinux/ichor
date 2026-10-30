package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.FilterChip
import androidx.compose.material3.IconToggleButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.model.LOG_SINCE_CHOICES
import name.levis.ichor.model.LogFilter

/** How long after the last key the filter is applied: each change reads the log again. */
private const val FILTER_DEBOUNCE_MS = 500L

/**
 * A text or regular expression filter and the time ranges, for a log sheet. [filter] is what
 * was applied; the field keeps its own text so typing stays smooth, and [onChange] follows it.
 */
@Composable
fun LogFilterBar(filter: LogFilter, onChange: (LogFilter) -> Unit, modifier: Modifier = Modifier) {
    var text by rememberSaveable { mutableStateOf(filter.text) }
    LaunchedEffect(text) {
        if (text != filter.text) {
            delay(FILTER_DEBOUNCE_MS)
            onChange(filter.copy(text = text))
        }
    }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedTextField(
            value = text,
            onValueChange = { text = it },
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            singleLine = true,
            placeholder = { Text(stringResource(R.string.pod_logs_filter_hint)) },
            trailingIcon = {
                val regexLabel = stringResource(R.string.pod_logs_filter_regex)
                IconToggleButton(
                    checked = filter.regex,
                    onCheckedChange = { onChange(filter.copy(regex = it)) },
                    modifier = Modifier.semantics { contentDescription = regexLabel },
                ) {
                    Text(".*", fontFamily = FontFamily.Monospace)
                }
            },
        )
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(LOG_SINCE_CHOICES, key = { "since-$it" }) { minutes ->
                FilterChip(
                    selected = filter.sinceMinutes == minutes,
                    onClick = { onChange(filter.copy(sinceMinutes = minutes)) },
                    label = { Text(sinceLabel(minutes)) },
                )
            }
        }
    }
}

@Composable
private fun sinceLabel(minutes: Int): String = when (minutes) {
    0 -> stringResource(R.string.pod_logs_since_all)
    in 1 until 60 -> stringResource(R.string.pod_logs_since_minutes, minutes)
    else -> stringResource(R.string.pod_logs_since_hours, minutes / 60)
}
