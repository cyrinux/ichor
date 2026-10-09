package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle

/** Typing pauses this long before the extra words apply (each change refetches everything). */
private const val WORDS_DEBOUNCE_MS = 800L

/** Screenshot mode: the Go core masks addresses, names and extra words in all it returns. */
@Composable
fun PrivacySection(app: TalosApp) {
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    var words by rememberSaveable { mutableStateOf(mask.extraWords) }
    fun applyWords() = app.setPrivacyMask(mask.copy(extraWords = words))

    LaunchedEffect(words) {
        delay(WORDS_DEBOUNCE_MS)
        applyWords()
    }

    SectionTitle(stringResource(R.string.settings_section_privacy))
    Card(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.settings_screenshot_mode), style = MaterialTheme.typography.titleMedium)
                MutedText(stringResource(R.string.settings_screenshot_mode_desc))
            }
            Switch(
                checked = mask.enabled,
                onCheckedChange = { app.setPrivacyMask(mask.copy(enabled = it, extraWords = words)) },
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        if (mask.enabled) {
            OutlinedTextField(
                value = words,
                onValueChange = { words = it },
                label = { Text(stringResource(R.string.settings_screenshot_mode_words)) },
                supportingText = { Text(stringResource(R.string.settings_screenshot_mode_words_hint)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { applyWords() }),
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 16.dp),
            )
        }
    }
    RemoteAppIconsSetting(app)
}

/** Opt-in: icons the app does not bundle are downloaded by their public name only. */
@Composable
private fun RemoteAppIconsSetting(app: TalosApp) {
    val enabled by app.uiPreferences.remoteAppIcons.collectAsStateWithLifecycle()
    Card(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.settings_remote_icons), style = MaterialTheme.typography.titleMedium)
                MutedText(stringResource(R.string.settings_remote_icons_desc))
            }
            Switch(checked = enabled, onCheckedChange = app.uiPreferences::setRemoteAppIcons, modifier = Modifier.padding(start = 12.dp))
        }
    }
}
