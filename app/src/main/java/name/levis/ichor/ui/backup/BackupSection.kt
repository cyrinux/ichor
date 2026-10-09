package name.levis.ichor.ui.backup

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.factory
import kotlinx.coroutines.launch

/** Settings: back up the clusters and settings to a passphrase-sealed file, or restore one over them. */
@Composable
fun BackupSection(hasConfig: Boolean, vm: BackupViewModel = backupViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    var confirmRestore by remember { mutableStateOf(false) }
    val pickBackup = BackupFlow(vm)
    val busy = state == BackupState.Sealing || state == BackupState.Restoring
    val backup = rememberBackupAction(vm)

    MutedText(stringResource(R.string.backup_desc))
    OutlinedButton(onClick = backup, enabled = hasConfig && !busy, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.backup_create))
    }
    OutlinedButton(onClick = { confirmRestore = true }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.backup_restore))
    }
    BackupStatus(state)

    if (confirmRestore) {
        ConfirmDialog(
            title = stringResource(R.string.backup_restore),
            text = stringResource(R.string.backup_restore_replace_warning),
            confirm = stringResource(R.string.backup_restore_pick),
            onConfirm = {
                confirmRestore = false
                pickBackup()
            },
            onDismiss = { confirmRestore = false },
        )
    }
}

/** The screen's backup view model: [BackupSection] and whoever starts a backup for it share it. */
@Composable
fun backupViewModel(): BackupViewModel = viewModel(factory = factory { BackupViewModel(app.backupManager) })

/**
 * Starts a backup on [vm]. The file holds the clusters' credentials: with the app lock on,
 * the owner proves it is them first.
 */
@Composable
fun rememberBackupAction(vm: BackupViewModel): () -> Unit {
    val context = LocalContext.current
    val appLock = (context.applicationContext as TalosApp).appLock
    val scope = rememberCoroutineScope()
    return remember(vm, context, scope) {
        {
            val activity = context.findFragmentActivity()
            if (!appLock.enabled.value || activity == null) {
                vm.startBackup()
            } else {
                scope.launch {
                    when (val auth = authenticate(activity, context.getString(R.string.backup_auth))) {
                        AuthResult.Success -> vm.startBackup()
                        is AuthResult.Failure -> vm.fail(UiText.Raw(auth.message))
                    }
                }
            }
        }
    }
}

/** First launch: restore a backup instead of importing a talosconfig. */
@Composable
fun RestoreBackupButton(onRestored: () -> Unit, vm: BackupViewModel = backupViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val pickBackup = BackupFlow(vm, onRestored)
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        TextButton(onClick = pickBackup, enabled = state != BackupState.Restoring) { Text(stringResource(R.string.backup_restore)) }
        if (state is BackupState.Failed) BackupStatus(state)
    }
}
