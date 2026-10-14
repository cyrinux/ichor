package name.levis.ichor.ui.dataservices

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.R
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.VeleroRestoreOptions
import name.levis.ichor.model.VeleroSchedule
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiText

/** Outcome of a Velero action on [target]: what it created (a Backup, a deletion request), or the error. */
data class VeleroActionResult(val deletion: Boolean, val target: String, val created: String, val error: UiText?)

/**
 * Velero backups now, deletions and restores, run in [scope] (a ViewModel's). [onChanged] runs
 * after one went through, to refresh the data services; [changes] tells the open backup lists
 * to reload.
 */
class VeleroActions(
    private val scope: CoroutineScope,
    private val dataServices: DataServicesRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<VeleroActionResult>(scope)

    /** Keys of the actions in flight: [backupKey] and [deleteKey]. */
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<VeleroActionResult> get() = actions.results

    private val _changes = MutableStateFlow(0)
    val changes: StateFlow<Int> = _changes.asStateFlow()

    /** The open backup lists reload, and the data services with them. */
    fun changed() {
        _changes.update { it + 1 }
        onChanged()
    }

    suspend fun backups(namespace: String, schedule: String) = dataServices.veleroBackups(namespace, schedule)

    /** What restoring [namespace]/[backup] holds; the same read follows a restore's progress. */
    suspend fun restorePreview(namespace: String, backup: String) = dataServices.veleroRestorePreview(namespace, backup)

    fun backupNow(schedule: VeleroSchedule) {
        actions.launch(backupKey(schedule), { dataServices.veleroBackupNow(schedule.namespace, schedule.name) }, {
            VeleroActionResult(false, schedule.label, it.getOrNull().orEmpty(), it.exceptionOrNull()?.uiText())
        }, ::changed)
    }

    fun delete(namespace: String, backup: String) {
        actions.launch(deleteKey(namespace, backup), { dataServices.veleroDeleteBackup(namespace, backup) }, {
            VeleroActionResult(true, backup, it.getOrNull().orEmpty(), it.exceptionOrNull()?.uiText())
        }, ::changed)
    }

    /** Restores [namespace]/[backup] with [options]: the Restore's name, whose progress the dialog follows. */
    suspend fun restore(namespace: String, backup: String, options: VeleroRestoreOptions): String =
        dataServices.veleroRestore(namespace, backup, options).also { changed() }

    companion object {
        fun backupKey(schedule: VeleroSchedule) = "now:${schedule.label}"
        fun deleteKey(namespace: String, backup: String) = "delete:$namespace/$backup"
    }
}

/** A toast for each outcome of [results]. */
@Composable
fun VeleroResultToasts(results: Flow<VeleroActionResult>) = ResultToasts(results) { context, r ->
    val text = r.error?.resolve(context)?.let { context.getString(R.string.velero_action_failed, r.target, it) }
        ?: if (r.deletion) context.getString(R.string.velero_delete_requested, r.target) else context.getString(R.string.velero_backup_now_started, r.created)
    text to (r.error != null)
}

/** Confirms a backup now: what it takes and where it goes, from the schedule's template. */
@Composable
fun VeleroBackupNowDialog(schedule: VeleroSchedule, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val namespaces = schedule.includedNamespaces.filter { it != "*" }.ifEmpty { null }?.joinToString(", ")
        ?: stringResource(R.string.velero_all_namespaces)
    ConfirmDialog(
        title = stringResource(R.string.velero_backup_now_title, schedule.label),
        text = stringResource(R.string.velero_backup_now_text, namespaces, schedule.storageLocation.ifEmpty { "-" }),
        confirm = stringResource(R.string.velero_backup_now),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    )
}

/** Deleting a backup removes its data: the backup's name is typed to confirm. */
@Composable
fun VeleroDeleteBackupDialog(namespace: String, backup: String, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val denial = rememberKubeDenial(KubeAction.VELERO_DELETE_BACKUP, namespace)
    HostnameConfirmDialog(
        title = stringResource(R.string.velero_delete_title, backup),
        hostname = backup,
        confirmLabel = stringResource(R.string.velero_delete),
        enabled = denial == null,
        emphasized = true,
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    ) {
        Text(stringResource(R.string.velero_delete_text))
        denial?.let { KubeDenialNote(it) }
    }
}
