package name.levis.ichor.ui.backup

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
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
fun BackupSection(hasConfig: Boolean, vm: BackupViewModel = viewModel(factory = factory { BackupViewModel(app.backupManager) })) {
    val context = LocalContext.current
    val appLock = (context.applicationContext as TalosApp).appLock
    val scope = rememberCoroutineScope()
    val state by vm.state.collectAsStateWithLifecycle()
    var confirmRestore by remember { mutableStateOf(false) }
    val pickBackup = BackupFlow(vm)
    val busy = state == BackupState.Sealing || state == BackupState.Restoring

    // The file holds the clusters' credentials: with the app lock on, prove it is the owner first.
    fun backup() {
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) return vm.startBackup()
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.backup_auth))) {
                AuthResult.Success -> vm.startBackup()
                is AuthResult.Failure -> vm.fail(UiText.Raw(auth.message))
            }
        }
    }

    MutedText(stringResource(R.string.backup_desc))
    OutlinedButton(onClick = ::backup, enabled = hasConfig && !busy, modifier = Modifier.fillMaxWidth()) {
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

/** First launch: restore a backup instead of importing a talosconfig. */
@Composable
fun RestoreBackupButton(onRestored: () -> Unit, vm: BackupViewModel = viewModel(factory = factory { BackupViewModel(app.backupManager) })) {
    val state by vm.state.collectAsStateWithLifecycle()
    val pickBackup = BackupFlow(vm, onRestored)
    OutlinedButton(onClick = pickBackup, enabled = state != BackupState.Restoring) { Text(stringResource(R.string.backup_restore)) }
    if (state is BackupState.Failed) BackupStatus(state)
}
