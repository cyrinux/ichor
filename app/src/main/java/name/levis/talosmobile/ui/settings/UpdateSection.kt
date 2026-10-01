package name.levis.talosmobile.ui.settings

import name.levis.talosmobile.ui.asString
import name.levis.talosmobile.R
import androidx.compose.ui.res.stringResource
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

    SectionTitle(stringResource(R.string.update_section))
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.update_auto_check), style = MaterialTheme.typography.titleMedium)
                    Text(
                        stringResource(R.string.update_auto_check_desc, BuildConfig.UPDATE_REPO),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(checked = autoCheck, onCheckedChange = updates::setAutoCheck, modifier = Modifier.padding(start = 12.dp))
            }

            when (val s = state) {
                UpdateState.Idle -> Unit
                UpdateState.Checking -> Text(stringResource(R.string.update_checking))
                UpdateState.UpToDate -> Text(stringResource(R.string.update_up_to_date, BuildConfig.VERSION_NAME))
                is UpdateState.Available -> {
                    Text(stringResource(R.string.update_available, s.info.version, formatBytes(s.info.apkSize)), style = MaterialTheme.typography.titleSmall)
                    if (s.info.notes.isNotBlank()) {
                        Text(s.info.notes.lines().take(8).joinToString("\n"), style = MaterialTheme.typography.bodySmall)
                    }
                    if (updates.canInstall) {
                        Button(onClick = { install(s.info) }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.update_download_install)) }
                    } else {
                        Text(
                            stringResource(R.string.update_debug_build),
                            style = MaterialTheme.typography.bodySmall,
                            color = LocalStatusColors.current.warn,
                        )
                    }
                }
                is UpdateState.Downloading -> {
                    Text(stringResource(R.string.update_downloading, s.info.version))
                    LinearProgressIndicator(progress = { s.progress }, modifier = Modifier.fillMaxWidth())
                }
                is UpdateState.NeedsInstallPermission -> {
                    Text(stringResource(R.string.update_needs_permission), style = MaterialTheme.typography.bodySmall)
                    OutlinedButton(onClick = {
                        context.startActivity(
                            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")),
                        )
                    }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.update_open_permission)) }
                    Button(onClick = { install(s.info) }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.update_try_again)) }
                }
                UpdateState.Installing -> Text(stringResource(R.string.update_installing))
                is UpdateState.Failed -> Text(s.message.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
            }

            val busy = state is UpdateState.Checking || state is UpdateState.Downloading || state is UpdateState.Installing
            OutlinedButton(
                onClick = { scope.launch { updates.check() } },
                enabled = !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.common_check_now)) }
        }
    }
}
