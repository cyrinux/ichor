package name.levis.ichor.ui.dataservices

import android.widget.Toast
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.CnpgCluster
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** Outcome of an on-demand backup of the cluster [label]: the new Backup's name, or the error. */
data class CnpgBackupResult(val label: String, val backup: String, val error: UiText?)

/**
 * On-demand CloudNativePG backups, run in [scope] (a ViewModel's). [onChanged] runs after a
 * backup was created, to refresh the data services.
 */
class CnpgActions(
    private val scope: CoroutineScope,
    private val talos: TalosRepository,
    private val onChanged: () -> Unit,
) {
    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    /** Labels of the clusters with a backup request in flight. */
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    // A queue, not a state: two backups finishing together each get their message.
    private val _results = Channel<CnpgBackupResult>(Channel.BUFFERED)
    val results: Flow<CnpgBackupResult> = _results.receiveAsFlow()

    fun backup(cluster: CnpgCluster) {
        val key = cluster.label
        if (key in _busy.value) return
        _busy.update { it + key }
        scope.launch {
            val outcome = runCatching { talos.cnpgBackup(cluster.namespace, cluster.name) }
            _busy.update { it - key }
            _results.send(CnpgBackupResult(key, outcome.getOrNull().orEmpty(), outcome.exceptionOrNull()?.uiText()))
            if (outcome.isSuccess) onChanged()
        }
    }
}

/** A toast for each outcome of [results]. */
@Composable
fun CnpgBackupToasts(results: Flow<CnpgBackupResult>) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r ->
            val text = r.error?.resolve(context)?.let { context.getString(R.string.cnpg_backup_failed, r.label, it) }
                ?: context.getString(R.string.cnpg_backup_started, r.backup)
            Toast.makeText(context, text, if (r.error != null) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
        }
    }
}

/** Confirms an on-demand backup: it loads the instance it runs on and fills the object store. */
@Composable
fun CnpgBackupConfirmDialog(cluster: CnpgCluster, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.cnpg_backup_title, cluster.label)) },
        text = { Text(stringResource(R.string.cnpg_backup_text)) },
        confirmButton = { TextButton(onClick = onConfirm) { Text(stringResource(R.string.cnpg_backup_now)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
