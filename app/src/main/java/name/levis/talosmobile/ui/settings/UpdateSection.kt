package name.levis.talosmobile.ui.settings

import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.talosmobile.BuildConfig
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.update.UpdateInfo
import name.levis.talosmobile.update.UpdateManager
import name.levis.talosmobile.update.UpdateState
import name.levis.talosmobile.util.formatBytes
import kotlinx.coroutines.launch

@Composable
fun UpdateSection(updates: UpdateManager) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val state by updates.state.collectAsStateWithLifecycle()
    val autoCheck by updates.autoCheck.collectAsStateWithLifecycle()

    fun install(info: UpdateInfo) = scope.launch { updates.downloadAndInstall(info) }

    SectionTitle("Updates")
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Check for updates daily", style = MaterialTheme.typography.titleMedium)
                    Text(
                        "From GitHub releases (${BuildConfig.UPDATE_REPO}). The APK's checksum and signing " +
                            "key are verified, and Android asks you to confirm the install.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(checked = autoCheck, onCheckedChange = updates::setAutoCheck, modifier = Modifier.padding(start = 12.dp))
            }

            when (val s = state) {
                UpdateState.Idle -> Unit
                UpdateState.Checking -> Text("Checking…")
                UpdateState.UpToDate -> Text("Talosdev Mobile ${BuildConfig.VERSION_NAME} is up to date.")
                is UpdateState.Available -> {
                    Text("Version ${s.info.version} is available (${formatBytes(s.info.apkSize)}).", style = MaterialTheme.typography.titleSmall)
                    if (s.info.notes.isNotBlank()) {
                        Text(s.info.notes.lines().take(8).joinToString("\n"), style = MaterialTheme.typography.bodySmall)
                    }
                    if (updates.canInstall) {
                        Button(onClick = { install(s.info) }, modifier = Modifier.fillMaxWidth()) { Text("Download and install") }
                    } else {
                        Text(
                            "This is a debug build, signed with a local key: release APKs cannot replace it. " +
                                "Uninstall it and install a release APK once to get updates.",
                            style = MaterialTheme.typography.bodySmall,
                            color = LocalStatusColors.current.warn,
                        )
                    }
                }
                is UpdateState.Downloading -> {
                    Text("Downloading ${s.info.version}…")
                    LinearProgressIndicator(progress = { s.progress }, modifier = Modifier.fillMaxWidth())
                }
                is UpdateState.NeedsInstallPermission -> {
                    Text("Allow Talosdev Mobile to install apps, then try again.", style = MaterialTheme.typography.bodySmall)
                    OutlinedButton(onClick = {
                        context.startActivity(
                            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")),
                        )
                    }, modifier = Modifier.fillMaxWidth()) { Text("Open install permission") }
                    Button(onClick = { install(s.info) }, modifier = Modifier.fillMaxWidth()) { Text("Try again") }
                }
                UpdateState.Installing -> Text("Installing… confirm in the system dialog.")
                is UpdateState.Failed -> Text(s.message, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
            }

            val busy = state is UpdateState.Checking || state is UpdateState.Downloading || state is UpdateState.Installing
            OutlinedButton(
                onClick = { scope.launch { updates.check() } },
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text("Check now") }
        }
    }
}
