package name.levis.ichor.ui.health

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
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.canAsk
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The health check helper, under a failed run when AI is enabled: explains the failure in
 * a few lines from what the check said, and leads to the full diagnosis.
 */
@Composable
fun HealthExplainCard(
    explain: ExplainState,
    onExplain: (language: String) -> Unit,
    onStop: () -> Unit,
    onContinue: () -> Unit,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val settings by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val providerName = app.diagnosisRepository.providers.firstOrNull { it.id == settings.provider }?.name ?: settings.provider
    val language = LocalConfiguration.current.locales[0].language
    // Read on every composition: the key may have been added in Settings a moment ago.
    val askable = canAsk(app.aiPreferences.apiKey(settings.provider), settings.baseUrl)

    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.ai_health_explain_title), style = MaterialTheme.typography.titleMedium)
            when {
                explain.running -> ExplainProgress(providerName, onStop)
                askable -> {
                    MutedText(stringResource(R.string.ai_health_explain_hint, providerName))
                    Button(onClick = { onExplain(language) }, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.ai_health_explain))
                    }
                }
                else -> MutedText(stringResource(R.string.ai_no_key))
            }
            ExplainAnswer(explain)
            OutlinedButton(onClick = onContinue, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.ai_health_continue))
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
        TextButton(onClick = onStop) { Text(stringResource(R.string.ai_stop)) }
    }
}

@Composable
private fun ExplainAnswer(explain: ExplainState) {
    val context = LocalContext.current
    if (explain.text.isNotEmpty()) {
        // Bounded so the check's own lines stay on screen below it.
        SelectionContainer(Modifier.heightIn(max = 280.dp).verticalScroll(rememberScrollState())) {
            Text(explain.text, style = MaterialTheme.typography.bodyMedium)
        }
    }
    explain.error?.let {
        Text(stringResource(R.string.common_stream_failed, it), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium)
    }
    if (explain.text.isNotEmpty() && !explain.running) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            MutedText(stringResource(R.string.ai_disclaimer), Modifier.weight(1f))
            TextButton(onClick = { copyToClipboard(context, "answer", explain.text) }) { Text(stringResource(R.string.ai_copy)) }
        }
    }
}
