package name.levis.ichor.ui.metrics

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.IconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
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
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.canAsk
import name.levis.ichor.model.PanelSuggestion
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.PromSource
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The panel assistant: a chat with the model set up in Settings that proposes panels for
 * [source], each checked against it by the Go core. [current] is the panel being edited
 * (its query goes to the model as context), null for a new one; [onUse] takes a proposed
 * panel into the editor.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PanelChatDialog(
    fingerprint: String,
    source: PromSource,
    current: PromPanel?,
    onUse: (PanelSuggestion) -> Unit,
    onSettings: () -> Unit,
    onDismiss: () -> Unit,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: PanelChatViewModel = viewModel(
        key = "panel-chat-$fingerprint",
        factory = factory { PanelChatViewModel(app.metricsChatRepository, app.aiPreferences) },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    val settings by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val language = LocalConfiguration.current.locales[0].language
    val providerName = app.diagnosisRepository.providers.firstOrNull { it.id == settings.provider }?.name ?: settings.provider
    // Read on every composition: the key may have been added in Settings a moment ago.
    val askable = canAsk(app.aiPreferences.apiKey(settings.provider), settings.baseUrl)
    var input by rememberSaveable { mutableStateOf("") }
    val listState = rememberLazyListState()

    LaunchedEffect(source, current) { vm.open(source, current) }
    // The newest text stays in view while the answer is written.
    LaunchedEffect(state.messages.lastOrNull()?.text, state.messages.size) {
        if (state.messages.isNotEmpty()) listState.animateScrollToItem(state.messages.size - 1)
    }

    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        Surface(Modifier.fillMaxSize().systemBarsPadding().imePadding()) {
            Scaffold(
                contentWindowInsets = WindowInsets(0, 0, 0, 0),
                topBar = {
                    TopAppBar(
                        title = { Text(stringResource(R.string.metrics_ai_title)) },
                        navigationIcon = { TooltipIconButton(Icons.Outlined.Close, stringResource(R.string.common_close), onClick = onDismiss) },
                        actions = {
                            if (state.messages.isNotEmpty()) {
                                TextButton(onClick = vm::newChat) { Text(stringResource(R.string.metrics_ai_new_chat)) }
                            }
                        },
                    )
                },
                bottomBar = {
                    if (askable) {
                        InputBar(
                            value = input,
                            onValueChange = { input = it },
                            enabled = state.ready && !state.asking,
                            asking = state.asking,
                            onSend = { vm.send(input, language); input = "" },
                            onStop = vm::stop,
                        )
                    }
                },
            ) { padding ->
                LazyColumn(
                    Modifier.padding(padding).fillMaxSize(),
                    state = listState,
                    contentPadding = PaddingValues(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    item {
                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            MutedText(stringResource(R.string.metrics_ai_notice, providerName))
                            val openError = state.error
                            when {
                                openError != null -> Text(openError, color = LocalStatusColors.current.bad)
                                !state.ready -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                                    Text(stringResource(R.string.metrics_ai_names_loading), style = MaterialTheme.typography.bodySmall)
                                }
                                state.namesFailed -> MutedText(stringResource(R.string.metrics_ai_names_failed))
                            }
                            if (!askable) {
                                MutedText(stringResource(R.string.ai_no_key))
                                OutlinedButton(onClick = onSettings, modifier = Modifier.fillMaxWidth()) {
                                    Text(stringResource(R.string.overview_action_settings))
                                }
                            } else if (state.messages.isEmpty()) {
                                Text(stringResource(R.string.metrics_ai_empty), style = MaterialTheme.typography.bodyMedium)
                            }
                        }
                    }
                    items(state.messages, key = { it.id }) { message ->
                        if (message.fromUser) UserBubble(message.text) else AnswerBubble(message, providerName, state.asking, source.label, onUse)
                    }
                    if (state.messages.any { !it.fromUser && it.text.isNotEmpty() }) {
                        item { MutedText(stringResource(R.string.ai_disclaimer)) }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun InputBar(value: String, onValueChange: (String) -> Unit, enabled: Boolean, asking: Boolean, onSend: () -> Unit, onStop: () -> Unit) {
    Surface(tonalElevation = 2.dp) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.Bottom,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            OutlinedTextField(
                value,
                onValueChange,
                label = { Text(stringResource(R.string.metrics_ai_input)) },
                maxLines = 5,
                modifier = Modifier.weight(1f),
            )
            if (asking) {
                TextButton(onClick = onStop) { Text(stringResource(R.string.ai_stop)) }
            } else {
                IconButton(onClick = onSend, enabled = enabled && value.isNotBlank()) {
                    Icon(Icons.AutoMirrored.Outlined.Send, stringResource(R.string.metrics_ai_send))
                }
            }
        }
    }
}

@Composable
private fun UserBubble(text: String) {
    Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.CenterEnd) {
        Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)) {
            Text(text, Modifier.padding(12.dp), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** The model's answer: its explanation as it is written, then the panel it proposed. */
@Composable
private fun AnswerBubble(message: ChatMessage, providerName: String, asking: Boolean, sourceLabel: String, onUse: (PanelSuggestion) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (message.text.isEmpty() && message.error == null && asking) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.ai_asking, providerName), style = MaterialTheme.typography.bodySmall)
            }
        }
        if (message.text.isNotEmpty()) {
            SelectionContainer { Text(message.text, style = MaterialTheme.typography.bodyMedium) }
        }
        message.error?.let {
            Text(stringResource(R.string.common_stream_failed, it), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium)
        }
        message.panel?.let { PanelSuggestionCard(it, sourceLabel, onUse) }
    }
}

@Composable
private fun PanelSuggestionCard(panel: PanelSuggestion, sourceLabel: String, onUse: (PanelSuggestion) -> Unit) {
    val context = LocalContext.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(panel.title.ifBlank { panel.query }, style = MaterialTheme.typography.titleSmall)
            SelectionContainer {
                Text(panel.query, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            }
            MutedText(
                stringResource(R.string.metrics_panel_unit, unitLabel(panel.unit)) +
                    panel.legend.takeIf { it.isNotBlank() }?.let { " · " + stringResource(R.string.metrics_panel_legend) + ": " + it }.orEmpty(),
            )
            val status = when {
                panel.verified && panel.empty -> stringResource(R.string.metrics_ai_verified_empty)
                panel.verified -> stringResource(R.string.metrics_ai_verified, sourceLabel)
                else -> stringResource(R.string.metrics_ai_unverified, panel.notice)
            }
            Text(
                status,
                style = MaterialTheme.typography.bodySmall,
                color = if (panel.verified && !panel.empty) LocalStatusColors.current.ok else MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (panel.attempts > 1 && panel.verified) MutedText(stringResource(R.string.metrics_ai_fixed))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Button(onClick = { onUse(panel) }) { Text(stringResource(R.string.metrics_ai_use)) }
                TextButton(onClick = { copyToClipboard(context, "query", panel.query) }) { Text(stringResource(R.string.ai_copy)) }
            }
        }
    }
}
