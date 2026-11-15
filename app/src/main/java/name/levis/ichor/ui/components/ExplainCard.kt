package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
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
import kotlinx.serialization.json.JsonObject
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.diagnosis.aiTarget
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.nav.LocalOpenDiagnosis
import name.levis.ichor.util.formatBytes

/**
 * "Explain with AI" for one pod, alert or checkup finding, on its sheet when AI is enabled: the
 * report it would send (read at once, nothing leaves the phone), the answer on a tap, and the
 * full diagnosis with [note] (the subject in words) already written. [onLeave] closes the sheets
 * it is on first: a screen pushed from an open sheet would show behind it.
 */
@Composable
fun ExplainCard(kind: ExplainKind, subject: JsonObject, note: String, onLeave: () -> Unit = {}) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: ExplainViewModel = viewModel(
        key = "explain:${kind.id}:$subject",
        factory = factory { ExplainViewModel(kind, subject, app.diagnosisRepository, app.aiPreferences) },
    )
    val settings by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val report by vm.report.collectAsStateWithLifecycle()
    val explain by vm.explain.collectAsStateWithLifecycle()
    val openDiagnosis = LocalOpenDiagnosis.current
    // Again when the setting changes: the report shown must be the one sent.
    LaunchedEffect(settings.anonymize) { vm.load() }

    ExplainCardContent(
        title = stringResource(R.string.ai_explain_title),
        hint = { provider, onPhone ->
            if (onPhone) stringResource(R.string.ai_explain_hint_device) else stringResource(kind.hint, provider)
        },
        explain = explain,
        // Nothing to send before the report is read.
        canExplain = !report.loading && report.error == null,
        onExplain = { language -> vm.explain(language, note) },
        onStop = vm::stop,
        onContinue = openDiagnosis?.let { open ->
            {
                onLeave()
                open(note)
            }
        },
    ) {
        ExplainReportSection(report, onRetry = vm::load)
    }
}

/**
 * The body every Explain with AI card shares: [title], what it sends ([hint] gets the provider's
 * name and whether the model is on the phone), the button, the streamed answer with Stop and Copy,
 * then [report] and "Continue in Diagnosis" ([onContinue]; none when nothing can open it).
 */
@Composable
fun ExplainCardContent(
    title: String,
    hint: @Composable (provider: String, onPhone: Boolean) -> String,
    explain: ExplainState,
    onExplain: (language: String) -> Unit,
    onStop: () -> Unit,
    onContinue: (() -> Unit)?,
    canExplain: Boolean = true,
    report: @Composable () -> Unit = {},
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val settings by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val target = aiTarget(app, settings)
    val language = LocalConfiguration.current.locales[0].language

    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            when {
                explain.running -> ExplainProgress(target.name, onStop)
                target.askable -> {
                    MutedText(hint(target.name, target.onPhone))
                    IchorButton(onClick = { onExplain(language) }, enabled = canExplain, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.ai_health_explain))
                    }
                }
                else -> MutedText(stringResource(target.notReadyText))
            }
            ExplainAnswer(explain)
            report()
            onContinue?.let {
                IchorOutlinedButton(onClick = it, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.ai_health_continue))
                }
            }
        }
    }
}

/** The report about to be sent, open until the first answer comes so it is read before the tap. */
@Composable
private fun ExplainReportSection(report: ExplainReport, onRetry: () -> Unit) {
    var expanded by rememberSaveable { mutableStateOf(true) }
    when {
        report.loading -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            MutedText(stringResource(R.string.ai_collecting))
        }
        report.error != null -> LoadFailureRow(report.error, onRetry = onRetry)
        else -> {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    stringResource(R.string.ai_report_title, formatBytes(report.text.encodeToByteArray().size.toLong())),
                    style = MaterialTheme.typography.labelLarge,
                    modifier = Modifier.weight(1f),
                )
                IchorTextButton(onClick = { expanded = !expanded }) {
                    Text(stringResource(if (expanded) R.string.ai_report_hide else R.string.ai_report_show))
                }
            }
            if (expanded) {
                MutedText(stringResource(if (report.anonymized) R.string.ai_notice_anonymized else R.string.ai_notice_real))
                // Bounded: a log tail of forty lines must not push the sheet's own rows away.
                SelectionContainer(Modifier.heightIn(max = 240.dp).verticalScroll(rememberScrollState())) {
                    Text(report.text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                }
            }
        }
    }
}

@Composable
private fun ExplainProgress(providerName: String, onStop: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        Text(
            stringResource(R.string.ai_asking, providerName),
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.weight(1f),
        )
        IchorTextButton(onClick = onStop) { Text(stringResource(R.string.ai_stop)) }
    }
}

@Composable
private fun ExplainAnswer(explain: ExplainState) {
    val context = LocalContext.current
    if (explain.text.isNotEmpty()) {
        // Bounded so what the card is about stays on screen below it.
        SelectionContainer(Modifier.heightIn(max = 280.dp).verticalScroll(rememberScrollState())) {
            Text(explain.text, style = MaterialTheme.typography.bodyMedium)
        }
    }
    // The Explain button above asks again.
    explain.error?.let { LoadFailureRow(it, onRetry = null) }
    if (explain.text.isNotEmpty() && !explain.running) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            MutedText(stringResource(R.string.ai_disclaimer), Modifier.weight(1f))
            IchorTextButton(onClick = { copyToClipboard(context, "answer", explain.text) }) { Text(stringResource(R.string.ai_copy)) }
        }
    }
}

/** [ExplainCard] in a sheet of its own, over the sheet or the screen that opened it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ExplainSheet(kind: ExplainKind, subject: JsonObject, note: String, onDismiss: () -> Unit, onLeave: () -> Unit = onDismiss) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, bottom = 24.dp)) {
            ExplainCard(kind, subject, note, onLeave = onLeave)
        }
    }
}
