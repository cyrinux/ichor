package dev.talos.viewer.ui.settings

import androidx.compose.foundation.layout.Column
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
import dev.talos.viewer.data.ThemeMode
import dev.talos.viewer.data.UiPreferences
import dev.talos.viewer.security.AppLock
import dev.talos.viewer.security.AuthResult
import dev.talos.viewer.security.authenticate
import dev.talos.viewer.security.canAuthenticate
import dev.talos.viewer.security.findFragmentActivity
import dev.talos.viewer.ui.components.SectionTitle
import dev.talos.viewer.ui.theme.LocalStatusColors
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppearanceSection(prefs: UiPreferences) {
    val mode by prefs.themeMode.collectAsStateWithLifecycle()
    SectionTitle("Appearance")
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        ThemeMode.entries.forEachIndexed { index, option ->
            SegmentedButton(
                selected = option == mode,
                onClick = { prefs.setThemeMode(option) },
                shape = SegmentedButtonDefaults.itemShape(index, ThemeMode.entries.size),
            ) { Text(option.label) }
        }
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
            error = "Set up a fingerprint or a screen lock (PIN, pattern, password) on this device first."
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
        authThen(if (target) "Enable app lock" else "Disable app lock") { appLock.setEnabled(target) }
    }

    SectionTitle("Security")
    Card(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("App lock", style = MaterialTheme.typography.titleMedium)
                Text(
                    "Fingerprint, or device PIN/pattern as fallback, to open the app and before reboot, " +
                        "shutdown and kubeconfig export. Blocks screenshots unless allowed below.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Switch(checked = enabled, onCheckedChange = ::toggle, modifier = Modifier.padding(start = 12.dp))
        }
        if (enabled) {
            Row(Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Allow screenshots", style = MaterialTheme.typography.titleSmall)
                    Text(
                        "Also shows the app content in recent apps.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(
                    checked = allowScreenshots,
                    onCheckedChange = { allow ->
                        if (allow) authThen("Allow screenshots") { prefs.setAllowScreenshots(true) }
                        else prefs.setAllowScreenshots(false)
                    },
                    modifier = Modifier.padding(start = 12.dp),
                )
            }
        }
    }
    error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
}
