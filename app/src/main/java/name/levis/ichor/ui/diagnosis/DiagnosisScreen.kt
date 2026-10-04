package name.levis.ichor.ui.diagnosis

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.canAsk
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * The optional AI diagnosis: shows the report that would be sent, then either asks the
 * configured model and shows its answer, or hands the question to an assistant app.
 * [initialNote] is set when opened from a failed health check.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiagnosisScreen(
    initialNote: String,
    onBack: () -> Unit,
    onSettings: () -> Unit,
    vm: DiagnosisViewModel = viewModel(factory = factory { DiagnosisViewModel(app.diagnosisRepository, app.aiPreferences) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val context = LocalContext.current
    val report by vm.report.collectAsStateWithLifecycle()
    val answer by vm.answer.collectAsStateWithLifecycle()
    val settings by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val language = LocalConfiguration.current.locales[0].language
    var note by rememberSaveable { mutableStateOf(initialNote) }

    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    // Bumped when screenshot mode changes: the report must then show the other names.
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val source = "$generation|${config?.activeContext}|$invalidations|${settings.anonymize}"
    LaunchedEffect(source) { vm.ensureCollected(source) }

    val provider = app.diagnosisRepository.providers.firstOrNull { it.id == settings.provider }
    val providerName = provider?.name ?: settings.provider
    val ready = report is UiState.Loaded
    // Read on every composition: the key may have been added in Settings a moment ago.
    val askable = canAsk(app.aiPreferences.apiKey(settings.provider), settings.baseUrl)

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.ai_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(
                        Icons.Outlined.Refresh,
                        stringResource(R.string.common_refresh),
                        onClick = vm::collect,
                        enabled = report !is UiState.Loading,
                    )
                },
            )
        },
    ) { padding ->
        Column(
            Modifier.padding(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            ReportCard(report, onRetry = vm::collect)
            OutlinedTextField(
                value = note,
                onValueChange = { note = it },
                label = { Text(stringResource(R.string.ai_note_label)) },
                minLines = 2,
                modifier = Modifier.fillMaxWidth(),
            )
            if (askable) {
                Button(
                    onClick = { vm.ask(note, language) },
                    enabled = ready && !answer.asking,
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.ai_ask, providerName)) }
            } else {
                MutedText(stringResource(R.string.ai_no_key))
                OutlinedButton(onClick = onSettings, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.overview_action_settings))
                }
            }
            OutlinedButton(
                onClick = { vm.prompt(note, language)?.let { shareText(context, it) } },
                enabled = ready,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.ai_share)) }
            MutedText(stringResource(R.string.ai_share_hint))
            AnswerCard(answer, providerName, onStop = vm::stop)
        }
    }
}

@Composable
private fun ReportCard(report: UiState<Report>, onRetry: () -> Unit) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            when (report) {
                UiState.Loading -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                    Text(stringResource(R.string.ai_collecting))
                }
                is UiState.Failed -> {
                    Text(report.message.asString(), color = LocalStatusColors.current.bad)
                    OutlinedButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
                }
                is UiState.Loaded -> {
                    val data = report.data
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            stringResource(R.string.ai_report_title, formatBytes(data.text.encodeToByteArray().size.toLong())),
                            style = MaterialTheme.typography.titleMedium,
                            modifier = Modifier.weight(1f),
                        )
                        TextButton(onClick = { expanded = !expanded }) {
                            Text(stringResource(if (expanded) R.string.ai_report_hide else R.string.ai_report_show))
                        }
                    }
                    MutedText(stringResource(if (data.anonymized) R.string.ai_notice_anonymized else R.string.ai_notice_real))
                    if (expanded) {
                        SelectionContainer {
                            Text(data.text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun AnswerCard(answer: AnswerState, providerName: String, onStop: () -> Unit) {
    if (!answer.asking && answer.text.isEmpty() && answer.error == null) return
    val context = LocalContext.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.ai_answer_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                when {
                    answer.asking -> TextButton(onClick = onStop) { Text(stringResource(R.string.ai_stop)) }
                    answer.text.isNotEmpty() -> TextButton(onClick = { copyText(context, answer.text) }) { Text(stringResource(R.string.ai_copy)) }
                }
            }
            if (answer.asking) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                    Text(stringResource(R.string.ai_asking, providerName), style = MaterialTheme.typography.bodySmall)
                }
            }
            if (answer.text.isNotEmpty()) {
                SelectionContainer { Text(answer.text, style = MaterialTheme.typography.bodyMedium) }
            }
            answer.error?.let {
                Text(stringResource(R.string.common_stream_failed, it), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium)
            }
            if (answer.text.isNotEmpty()) {
                MutedText(stringResource(R.string.ai_disclaimer))
            }
        }
    }
}

/** Opens the share sheet with [text], for the assistant app the user picks. */
private fun shareText(context: Context, text: String) {
    val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text)
    try {
        context.startActivity(Intent.createChooser(send, context.getString(R.string.ai_share)))
    } catch (_: ActivityNotFoundException) {
        // No app takes text: the report can still be selected and copied.
    } catch (_: RuntimeException) {
        // A report too large for an intent (TransactionTooLargeException): same fallback.
    }
}

private fun copyText(context: Context, text: String) {
    context.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("answer", text))
}
