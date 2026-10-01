package name.levis.talosmobile.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.RadioButton
import androidx.compose.material3.TextButton
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import name.levis.talosmobile.R
import name.levis.talosmobile.i18n.AppLocale
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.talosmobile.data.ThemeMode
import name.levis.talosmobile.data.UiPreferences
import name.levis.talosmobile.security.AppLock
import name.levis.talosmobile.security.AuthResult
import name.levis.talosmobile.security.authenticate
import name.levis.talosmobile.security.canAuthenticate
import name.levis.talosmobile.security.findFragmentActivity
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.theme.LocalStatusColors
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppearanceSection(prefs: UiPreferences) {
    val mode by prefs.themeMode.collectAsStateWithLifecycle()
    SectionTitle(stringResource(R.string.settings_section_appearance))
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        ThemeMode.entries.forEachIndexed { index, option ->
            SegmentedButton(
                selected = option == mode,
                onClick = { prefs.setThemeMode(option) },
                shape = SegmentedButtonDefaults.itemShape(index, ThemeMode.entries.size),
            ) { Text(stringResource(option.label)) }
        }
    }
    LanguageSetting(prefs)
}

/** In-app language: system default or one of the translations, each named in its own language. */
@Composable
private fun LanguageSetting(prefs: UiPreferences) {
    val context = LocalContext.current
    val current by prefs.language.collectAsStateWithLifecycle()
    var picking by remember { mutableStateOf(false) }
    val systemLabel = stringResource(R.string.settings_language_system)
    fun labelOf(tag: String) = AppLocale.languages.firstOrNull { it.tag == tag }?.nativeName ?: systemLabel

    Card(Modifier.fillMaxWidth().clickable { picking = true }) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.settings_language), style = MaterialTheme.typography.titleMedium)
            Text(
                labelOf(current),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }

    if (picking) {
        val options = listOf(AppLocale.SYSTEM) + AppLocale.languages.map { it.tag }
        AlertDialog(
            onDismissRequest = { picking = false },
            title = { Text(stringResource(R.string.settings_language)) },
            text = {
                Column(Modifier.selectableGroup()) {
                    options.forEach { tag ->
                        Row(
                            Modifier.fillMaxWidth()
                                .selectable(selected = tag == current, role = Role.RadioButton, onClick = {
                                    picking = false
                                    if (tag != current) {
                                        prefs.setLanguage(tag)
                                        if (AppLocale.apply(context, tag)) context.findFragmentActivity()?.recreate()
                                    }
                                })
                                .padding(vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = tag == current, onClick = null)
                            Text(labelOf(tag), modifier = Modifier.padding(start = 12.dp))
                        }
                    }
                }
            },
            confirmButton = { TextButton(onClick = { picking = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/** App-lock switch. Both enabling and disabling require authenticating first. */
@Composable
fun SecuritySection(appLock: AppLock, prefs: UiPreferences) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val enabled by appLock.enabled.collectAsStateWithLifecycle()
    val allowScreenshots by prefs.allowScreenshots.collectAsStateWithLifecycle()
    var error by remember { mutableStateOf<String?>(null) }

    /** Runs [action] after a fingerprint/PIN check. */
    fun authThen(title: String, action: () -> Unit) {
        val activity = context.findFragmentActivity() ?: return
        if (!canAuthenticate(context)) {
            error = context.getString(R.string.settings_auth_needs_lock)
            return
        }
        scope.launch {
            when (val result = authenticate(activity, title)) {
                AuthResult.Success -> {
                    error = null
                    action()
                }
                is AuthResult.Failure -> error = result.message
            }
        }
    }

    fun toggle(target: Boolean) {
        authThen(context.getString(if (target) R.string.settings_auth_enable_lock else R.string.settings_auth_disable_lock)) { appLock.setEnabled(target) }
    }

    SectionTitle(stringResource(R.string.settings_section_security))
    Card(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.settings_app_lock), style = MaterialTheme.typography.titleMedium)
                Text(
                    stringResource(R.string.settings_app_lock_desc),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Switch(checked = enabled, onCheckedChange = ::toggle, modifier = Modifier.padding(start = 12.dp))
        }
        if (enabled) {
            Row(Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.settings_allow_screenshots), style = MaterialTheme.typography.titleSmall)
                    Text(
                        stringResource(R.string.settings_allow_screenshots_desc),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(
                    checked = allowScreenshots,
                    onCheckedChange = { allow ->
                        if (allow) authThen(context.getString(R.string.settings_allow_screenshots)) { prefs.setAllowScreenshots(true) }
                        else prefs.setAllowScreenshots(false)
                    },
                    modifier = Modifier.padding(start = 12.dp),
                )
            }
        }
    }
    error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
}
