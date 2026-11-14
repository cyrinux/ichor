package name.levis.ichor.ui.workloads

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.model.LOG_TEMPLATES_MIN_LINES
import name.levis.ichor.model.LOG_TEMPLATE_HOLE
import name.levis.ichor.model.LogLevel
import name.levis.ichor.model.LogTemplateGroup
import name.levis.ichor.model.LogTemplates
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * "Top messages" of a loaded log: its repeated messages with their count and share, the parts
 * that change from line to line (numbers, ids, addresses, durations) dimmed. A tap closes the
 * sheet and filters the log on that message ([onPick] gets its newest line as written). [load]
 * groups the lines once, off the main thread; [lineCount] under [LOG_TEMPLATES_MIN_LINES] shows
 * why there is nothing to group.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LogTemplatesSheet(lineCount: Int, load: suspend () -> LogTemplates, onPick: (String) -> Unit, onDismiss: () -> Unit) {
    var templates by remember { mutableStateOf<LogTemplates?>(null) }
    var failed by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        if (lineCount < LOG_TEMPLATES_MIN_LINES) return@LaunchedEffect
        try {
            templates = load()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            failed = e.message.orEmpty()
        }
    }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.log_templates_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(horizontal = 16.dp))
            val shown = templates
            when {
                lineCount < LOG_TEMPLATES_MIN_LINES -> MutedText(stringResource(R.string.log_templates_empty, LOG_TEMPLATES_MIN_LINES), Modifier.padding(16.dp))
                failed != null -> MutedText(failed.orEmpty(), Modifier.padding(16.dp))
                shown == null -> LinearProgressIndicator(Modifier.fillMaxWidth().padding(horizontal = 16.dp))
                else -> {
                    MutedText(stringResource(R.string.log_templates_filter_hint), Modifier.padding(horizontal = 16.dp))
                    LazyColumn(contentPadding = PaddingValues(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        items(shown.groups, key = { "${it.level}|${it.source}|${it.template}" }) { group ->
                            TemplateRow(group) {
                                onPick(group.sample)
                                onDismiss()
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun TemplateRow(group: LogTemplateGroup, onClick: () -> Unit) {
    val color = levelColor(group.logLevel)
    val dim = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.55f)
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                stringResource(R.string.log_templates_count, group.count, group.shareText()),
                style = MaterialTheme.typography.labelMedium,
                color = color ?: MaterialTheme.colorScheme.onSurface,
            )
            if (group.pods > 0) MutedText(stringResource(R.string.log_templates_pods, group.pods))
        }
        LinearProgressIndicator(
            progress = { group.share.toFloat().coerceIn(0f, 1f) },
            modifier = Modifier.fillMaxWidth(),
            color = color ?: MaterialTheme.colorScheme.primary,
        )
        Text(templateText(group.template, dim), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
    }
}

/** The log rows' colour of [level] (error, warning), none for the others. */
@Composable
private fun levelColor(level: LogLevel): Color? = when (level) {
    LogLevel.ERROR -> MaterialTheme.colorScheme.error
    LogLevel.WARN -> LocalStatusColors.current.warn
    else -> null
}

/** [template] with its `<*>` placeholders in [dim]. */
private fun templateText(template: String, dim: Color): AnnotatedString = buildAnnotatedString {
    val parts = template.split(LOG_TEMPLATE_HOLE)
    parts.forEachIndexed { i, part ->
        append(part)
        if (i < parts.lastIndex) withStyle(SpanStyle(color = dim)) { append(LOG_TEMPLATE_HOLE) }
    }
}
