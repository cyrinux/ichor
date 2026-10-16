package name.levis.ichor.ui.incident

import androidx.annotation.StringRes
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.shareText
import name.levis.ichor.ui.userMessage

private const val HOUR_MILLIS = 3_600_000L

/** The windows the summary covers, back from now. */
enum class IncidentWindow(val millis: Long, @StringRes val label: Int) {
    HOURS_6(6 * HOUR_MILLIS, R.string.incident_window_6h),
    HOURS_24(24 * HOUR_MILLIS, R.string.incident_window_24h),
    DAYS_7(7 * 24 * HOUR_MILLIS, R.string.incident_window_7d),
}

/** What the sheet shows: the summary, or why there is none. */
private sealed interface SummaryState {
    data object Loading : SummaryState
    data class Ready(val markdown: String, val hasHistory: Boolean) : SummaryState
    data class Failed(val message: String) : SummaryState
}

/**
 * "Share incident summary…": an anonymised Markdown page about [cluster] over the chosen
 * window (what is wrong now, the timeline, the actions taken from the phone, the polling
 * gaps), built from what the phone holds only. Names are hidden by default; Share hands the
 * text to the share sheet, Copy to the clipboard.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun IncidentSummarySheet(cluster: ContextSummary, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    var window by remember { mutableStateOf(IncidentWindow.HOURS_24) }
    var hideNames by remember { mutableStateOf(true) }
    var state by remember { mutableStateOf<SummaryState>(SummaryState.Loading) }
    LaunchedEffect(cluster.fingerprint, cluster.name, window, hideNames) {
        state = SummaryState.Loading
        val now = System.currentTimeMillis()
        state = runCatching {
            val markdown = app.historyRepository.incidentSummary(cluster.fingerprint, cluster.name, now - window.millis, now, hideNames)
            SummaryState.Ready(markdown, app.historyRepository.hasHistory(cluster.fingerprint))
        }.getOrElse { SummaryState.Failed(it.userMessage()) }
    }
    val title = stringResource(R.string.incident_summary_title)
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(title, style = MaterialTheme.typography.titleLarge)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                IncidentWindow.entries.forEach { w ->
                    FilterChip(selected = window == w, onClick = { window = w }, label = { Text(stringResource(w.label)) })
                }
            }
            ToggleRow(stringResource(R.string.incident_hide_names), hideNames, { hideNames = it })
            when (val s = state) {
                SummaryState.Loading -> Box(Modifier.fillMaxWidth().padding(24.dp)) { CircularProgressIndicator() }
                is SummaryState.Failed -> InlineError(s.message)
                is SummaryState.Ready -> {
                    if (!s.hasHistory) MutedText(stringResource(R.string.incident_empty))
                    SummaryPreview(s.markdown)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(onClick = { shareText(context, s.markdown, title) }) {
                            Icon(Icons.Outlined.Share, contentDescription = null)
                            Text(stringResource(R.string.incident_share), Modifier.padding(start = 8.dp))
                        }
                        OutlinedButton(onClick = { copyToClipboard(context, title, s.markdown) }) {
                            Icon(Icons.Outlined.ContentCopy, contentDescription = null)
                            Text(stringResource(R.string.incident_copy), Modifier.padding(start = 8.dp))
                        }
                    }
                }
            }
        }
    }
}

/** The Markdown as it will be shared: monospace, scrollable both ways, selectable. */
@Composable
private fun SummaryPreview(markdown: String) {
    Surface(
        color = MaterialTheme.colorScheme.surfaceContainerHighest,
        shape = MaterialTheme.shapes.medium,
        modifier = Modifier.fillMaxWidth().heightIn(max = 360.dp),
    ) {
        SelectionContainer {
            Text(
                markdown,
                fontFamily = FontFamily.Monospace,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier
                    .verticalScroll(rememberScrollState())
                    .horizontalScroll(rememberScrollState())
                    .padding(12.dp),
            )
        }
    }
}
