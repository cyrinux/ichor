package name.levis.ichor.ui.alerts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.InputChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.model.AM_MAX_SILENCE_MINUTES
import name.levis.ichor.model.AM_SILENCE_PRESETS
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmMatcher
import name.levis.ichor.model.KubePermission
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.userMessage

/** Longest comment Alertmanager is sent (the Go core refuses more than 1000 bytes). */
private const val MAX_COMMENT = 500

/**
 * Silences [alert]: its matchers, prefilled to mute exactly it ([matchersFor]), shown as chips
 * that can be removed to widen the silence; how long (1 hour to 1 week, or a number of hours up
 * to 30 days); a required comment. [onSilence] gets them; the outcome is a toast. [initialMinutes]
 * and [initialComment] prefill the form (a notification's Silence 1 h).
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun SilenceSheet(
    alert: AmAlert,
    matchersFor: suspend (Map<String, String>) -> List<AmMatcher>,
    denial: KubePermission?,
    onSilence: (matchers: List<AmMatcher>, minutes: Long, comment: String) -> Unit,
    onDismiss: () -> Unit,
    initialMinutes: Long = AM_SILENCE_PRESETS.first(),
    initialComment: String = "",
) {
    var matchers by remember { mutableStateOf<List<AmMatcher>?>(null) }
    var matchersError by remember { mutableStateOf<String?>(null) }
    var minutes by rememberSaveable { mutableStateOf(initialMinutes) }
    var custom by rememberSaveable { mutableStateOf(initialMinutes !in AM_SILENCE_PRESETS) }
    var hoursText by rememberSaveable { mutableStateOf(if (custom) (initialMinutes / 60).toString() else "") }
    var comment by rememberSaveable { mutableStateOf(initialComment) }
    LaunchedEffect(alert.fingerprint) {
        matchers = try {
            matchersFor(alert.labels)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            matchersError = e.userMessage()
            emptyList()
        }
    }
    val chosen = if (custom) (hoursText.toLongOrNull() ?: 0L) * 60 else minutes
    val valid = !matchers.isNullOrEmpty() && comment.isNotBlank() && chosen in 1..AM_MAX_SILENCE_MINUTES && denial == null

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(stringResource(R.string.alerts_silence_title, alert.alertname), style = MaterialTheme.typography.titleLarge)
            Label(stringResource(R.string.alerts_silence_matchers))
            when (val current = matchers) {
                null -> LinearProgressIndicator(Modifier.fillMaxWidth())
                else -> {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        current.forEach { m ->
                            InputChip(
                                selected = false,
                                onClick = { matchers = current - m },
                                label = { Text(m.text, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.labelMedium) },
                                trailingIcon = {
                                    Icon(Icons.Outlined.Close, stringResource(R.string.alerts_silence_remove_matcher, m.name), Modifier.size(16.dp))
                                },
                            )
                        }
                    }
                    MutedText(matchersError ?: stringResource(if (current.isEmpty()) R.string.alerts_silence_no_matchers else R.string.alerts_silence_matchers_hint))
                }
            }
            Label(stringResource(R.string.alerts_silence_for))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                AM_SILENCE_PRESETS.forEach { m ->
                    FilterChip(selected = !custom && minutes == m, onClick = { minutes = m; custom = false }, label = { Text(silenceDurationLabel(m)) })
                }
                FilterChip(selected = custom, onClick = { custom = true }, label = { Text(stringResource(R.string.alerts_silence_custom)) })
            }
            if (custom) {
                OutlinedTextField(
                    value = hoursText,
                    onValueChange = { hoursText = it.filter(Char::isDigit).take(3) },
                    label = { Text(stringResource(R.string.alerts_silence_hours)) },
                    supportingText = { Text(stringResource(R.string.alerts_silence_hours_hint)) },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            OutlinedTextField(
                value = comment,
                onValueChange = { comment = it.take(MAX_COMMENT) },
                label = { Text(stringResource(R.string.alerts_silence_comment)) },
                supportingText = { Text(stringResource(R.string.alerts_silence_comment_hint)) },
                minLines = 2,
                modifier = Modifier.fillMaxWidth(),
            )
            Button(
                onClick = { matchers?.let { onSilence(it, chosen, comment.trim()) } },
                enabled = valid,
                modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
            ) {
                Icon(Icons.Outlined.NotificationsOff, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.alerts_silence), modifier = Modifier.padding(start = 8.dp))
            }
            KubeDenialNote(denial)
        }
    }
}

@Composable
private fun Label(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 4.dp))
}

/** "1 hour", "4 hours", "1 day", "1 week". */
@Composable
private fun silenceDurationLabel(minutes: Long): String {
    val hours = (minutes / 60).toInt()
    return when {
        hours % (24 * 7) == 0 -> pluralStringResource(R.plurals.alerts_weeks, hours / (24 * 7), hours / (24 * 7))
        hours % 24 == 0 -> pluralStringResource(R.plurals.alerts_days, hours / 24, hours / 24)
        else -> pluralStringResource(R.plurals.alerts_hours, hours, hours)
    }
}
