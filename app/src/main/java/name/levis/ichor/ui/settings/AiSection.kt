package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Card
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.AiModel
import name.levis.ichor.data.AiProvider
import name.levis.ichor.data.AiSettings
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/** The optional AI diagnosis: off by default; its entry points only exist while it is on. */
@Composable
fun AiSection(app: TalosApp) {
    val prefs = app.aiPreferences
    val settings by prefs.settings.collectAsStateWithLifecycle()

    SectionTitle(stringResource(R.string.ai_title))
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            SwitchRow(
                title = stringResource(R.string.ai_title),
                description = stringResource(R.string.ai_enable_desc),
                checked = settings.enabled,
                onChange = { prefs.update(settings.copy(enabled = it)) },
            )
            if (settings.enabled) {
                ProviderSettings(app, settings)
                SwitchRow(
                    title = stringResource(R.string.ai_anonymize),
                    description = stringResource(R.string.ai_anonymize_desc),
                    checked = settings.anonymize,
                    onChange = { prefs.update(settings.copy(anonymize = it)) },
                )
            }
        }
    }
}

@Composable
private fun SwitchRow(title: String, description: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            MutedText(description)
        }
        Switch(checked = checked, onCheckedChange = onChange, modifier = Modifier.padding(start = 12.dp))
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ProviderSettings(app: TalosApp, settings: AiSettings) {
    val prefs = app.aiPreferences
    val providers = app.diagnosisRepository.providers
    val provider = providers.firstOrNull { it.id == settings.provider } ?: return
    val uriHandler = LocalUriHandler.current
    // Per provider: the stored key is read once, then edits are written through.
    var apiKey by remember(provider.id) { mutableStateOf(prefs.apiKey(provider.id)) }

    Text(stringResource(R.string.ai_provider), style = MaterialTheme.typography.labelLarge)
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        providers.forEachIndexed { index, p ->
            SegmentedButton(
                selected = p.id == provider.id,
                onClick = { prefs.update(settings.copy(provider = p.id)) },
                shape = SegmentedButtonDefaults.itemShape(index, providers.size),
            ) { Text(p.name) }
        }
    }
    OutlinedTextField(
        value = apiKey,
        onValueChange = {
            apiKey = it
            prefs.setApiKey(provider.id, it)
        },
        label = { Text(stringResource(R.string.ai_api_key)) },
        supportingText = { Text(stringResource(R.string.ai_api_key_hint)) },
        singleLine = true,
        visualTransformation = PasswordVisualTransformation(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
    TextButton(onClick = { uriHandler.openUri(provider.keyUrl) }) { Text(stringResource(R.string.ai_get_key)) }
    ModelField(app, settings, provider, apiKey)
    OutlinedTextField(
        value = settings.baseUrl,
        onValueChange = { prefs.update(settings.withBaseUrl(it)) },
        label = { Text(stringResource(R.string.ai_base_url)) },
        supportingText = { Text(stringResource(R.string.ai_base_url_hint)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
}

/** The model id, typed or picked from the list the provider returns for this key. */
@Composable
private fun ModelField(app: TalosApp, settings: AiSettings, provider: AiProvider, apiKey: String) {
    val prefs = app.aiPreferences
    val scope = rememberCoroutineScope()
    var models by remember(provider.id) { mutableStateOf<List<AiModel>?>(null) }
    var loading by remember(provider.id) { mutableStateOf(false) }
    var error by remember(provider.id) { mutableStateOf<String?>(null) }

    OutlinedTextField(
        value = settings.model,
        onValueChange = { prefs.update(settings.withModel(it)) },
        label = { Text(stringResource(R.string.ai_model)) },
        placeholder = { Text(provider.defaultModel) },
        supportingText = { Text(stringResource(R.string.ai_model_default, provider.defaultModel)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
    Box {
        TextButton(
            enabled = !loading,
            onClick = {
                loading = true
                error = null
                scope.launch {
                    runCatching { app.diagnosisRepository.models(provider.id, apiKey, settings.baseUrl) }
                        .onSuccess { models = it }
                        .onFailure { error = it.userMessage() }
                    loading = false
                }
            },
        ) { Text(stringResource(R.string.ai_model_list)) }
        DropdownMenu(expanded = models != null, onDismissRequest = { models = null }) {
            if (models.isNullOrEmpty()) {
                DropdownMenuItem(text = { Text(stringResource(R.string.ai_model_none)) }, onClick = { models = null }, enabled = false)
            }
            models?.forEach { model ->
                DropdownMenuItem(
                    text = { Text(model.name.ifEmpty { model.id }) },
                    onClick = {
                        prefs.update(settings.withModel(model.id))
                        models = null
                    },
                )
            }
        }
    }
    error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
}
