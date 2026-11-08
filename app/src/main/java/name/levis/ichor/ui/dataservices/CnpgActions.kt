package name.levis.ichor.ui.dataservices

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.CnpgCluster
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.uiText

/** Outcome of an on-demand backup of the cluster [label]: the new Backup's name, or the error. */
data class CnpgBackupResult(val label: String, val backup: String, val error: UiText?)

/**
 * CloudNativePG actions (backups, promotion, restart and hibernation, instance removal), run
 * in [scope] (a ViewModel's). [onChanged] runs after an action was accepted, to refresh the
 * data services.
 */
class CnpgActions(
    private val scope: CoroutineScope,
    private val dataServices: DataServicesRepository,
    private val onChanged: () -> Unit,
) {
    private val lifecycle = KeyedActions<CnpgLifecycleResult>(scope)
    val lifecycleBusy: StateFlow<Set<String>> get() = lifecycle.busy
    val lifecycleResults: Flow<CnpgLifecycleResult> get() = lifecycle.results
    fun lifecycle(cluster: CnpgCluster, action: CnpgLifecycleAction) {
        if (running(cluster)) return
        lifecycle.launch(cluster.label, {
            when (action) {
                CnpgLifecycleAction.RESTART -> dataServices.cnpgRestartPrimary(cluster.namespace, cluster.name)
                CnpgLifecycleAction.HIBERNATE -> dataServices.cnpgHibernate(cluster.namespace, cluster.name, true)
                CnpgLifecycleAction.WAKE -> dataServices.cnpgHibernate(cluster.namespace, cluster.name, false)
            }
        }, { CnpgLifecycleResult(cluster.label, it.exceptionOrNull()?.uiText()) }, onChanged)
    }
    private val removal = KeyedActions<CnpgDestroyResult>(scope)
    val destroyBusy: StateFlow<Set<String>> get() = removal.busy
    val destroyResults: Flow<CnpgDestroyResult> get() = removal.results
    suspend fun destroyPreview(cluster: CnpgCluster, instance: String) = dataServices.cnpgDestroyPreview(cluster.namespace, cluster.name, instance)
    fun destroy(cluster: CnpgCluster, instance: String) {
        if (running(cluster)) return
        removal.launch(cluster.label, { dataServices.cnpgDestroy(cluster.namespace, cluster.name, instance) }, {
            CnpgDestroyResult(cluster.label, instance, it.exceptionOrNull()?.uiText())
        }, onChanged)
    }

    /** One action at a time per cluster. */
    private fun running(cluster: CnpgCluster): Boolean = cluster.label.let {
        it in busy.value || it in promoteBusy.value || it in lifecycleBusy.value || it in destroyBusy.value
    }

    private val promotion = KeyedActions<CnpgPromoteResult>(scope)
    val promoteBusy: StateFlow<Set<String>> get() = promotion.busy
    val promoteResults: Flow<CnpgPromoteResult> get() = promotion.results
    suspend fun preview(cluster: CnpgCluster, instance: String) = dataServices.cnpgPromotePreview(cluster.namespace, cluster.name, instance)
    fun promote(cluster: CnpgCluster, instance: String) {
        if (running(cluster)) return
        promotion.launch(cluster.label, { dataServices.cnpgPromote(cluster.namespace, cluster.name, instance) }, {
            CnpgPromoteResult(cluster.label, instance, it.exceptionOrNull()?.uiText())
        }, onChanged)
    }

    private val actions = KeyedActions<CnpgBackupResult>(scope)
    /** Labels of the clusters with a backup request in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy

    val results: Flow<CnpgBackupResult> get() = actions.results

    fun backup(cluster: CnpgCluster) {
        if (running(cluster)) return
        val key = cluster.label
        actions.launch(key, { dataServices.cnpgBackup(cluster.namespace, cluster.name) }, {
            CnpgBackupResult(key, it.getOrNull().orEmpty(), it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
}

/** A toast for each outcome of [results]. */
@Composable
fun CnpgBackupToasts(results: Flow<CnpgBackupResult>) = ResultToasts(results) { context, r ->
    val text = r.error?.resolve(context)?.let { context.getString(R.string.cnpg_backup_failed, r.label, it) }
        ?: context.getString(R.string.cnpg_backup_started, r.backup)
    text to (r.error != null)
}

/** Confirms an on-demand backup: it loads the instance it runs on and fills the object store. */
@Composable
fun CnpgBackupConfirmDialog(cluster: CnpgCluster, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.cnpg_backup_title, cluster.label),
        text = stringResource(R.string.cnpg_backup_text),
        confirm = stringResource(R.string.cnpg_backup_now),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    )
}
