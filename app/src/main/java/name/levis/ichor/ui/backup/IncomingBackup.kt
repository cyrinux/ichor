package name.levis.ichor.ui.backup

import android.net.Uri
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.model.looksLikeBackup
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.importconfig.MAX_CONFIG_BYTES
import name.levis.ichorgo.Ichorgo
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * A backup file opened from another app (a file manager, a mail): read once [uri] is given
 * ([onRead] then clears it), then its passphrase and the restore. The file is held by [vm]
 * from then on, so the dialogs survive a configuration change.
 * [hasConfig]: the passphrase dialog warns that the restore replaces the stored clusters.
 * A file that is a config instead (a kubeconfig or a talosconfig, shared or opened with the
 * app) goes to [onConfig], for the import preview.
 */
@Composable
fun IncomingBackup(
    uri: Uri?,
    hasConfig: Boolean,
    onRead: () -> Unit,
    onRestored: () -> Unit,
    onConfig: (String) -> Unit,
    vm: BackupViewModel = viewModel(key = "incoming-backup", factory = factory { BackupViewModel(app.backupManager) }),
) {
    val context = LocalContext.current
    val state by vm.state.collectAsStateWithLifecycle()

    LaunchedEffect(uri) {
        if (uri == null) return@LaunchedEffect
        runCatching { readBytes(context, uri) }.fold(
            onSuccess = { bytes ->
                val config = if (looksLikeBackup(bytes)) null else configText(bytes)
                when {
                    looksLikeBackup(bytes) -> vm.picked(bytes)
                    config != null -> onConfig(config)
                    else -> vm.fail(UiText.Res(R.string.backup_err_not_backup))
                }
            },
            onFailure = { vm.fail() },
        )
        onRead()
    }

    BackupFlow(vm, onRestored = onRestored, replaceWarning = hasConfig)
    when (val s = state) {
        BackupState.RestoreDone -> LaunchedEffect(s) { vm.reset() }
        is BackupState.Failed -> AlertDialog(
            onDismissRequest = vm::reset,
            title = { Text(stringResource(R.string.backup_restore_title)) },
            text = { Text(s.message.asString()) },
            confirmButton = { TextButton(onClick = vm::reset) { Text(stringResource(R.string.common_ok)) } },
        )
        else -> Unit
    }
}

/**
 * [file] as config text when it holds a kubeconfig or a talosconfig (plain, or an
 * "ichor-config:" payload); null otherwise, e.g. any other file opened with the app.
 */
private suspend fun configText(file: ByteArray): String? = withContext(Dispatchers.IO) {
    if (file.size > MAX_CONFIG_BYTES) return@withContext null
    val text = file.decodeToString()
    val yaml = runCatching { Ichorgo.decodeImportText(text) }.getOrNull() ?: return@withContext null
    text.takeIf { Ichorgo.isKubeconfig(yaml) || runCatching { Ichorgo.parseConfig(yaml) }.isSuccess }
}
