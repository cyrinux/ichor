package name.levis.ichor.ui.dataservices

import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.CnpgCluster
import name.levis.ichor.model.KubeAction
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog

enum class CnpgLifecycleAction(val title: Int, val risk: Int, val permission: KubeAction) {
    RESTART(R.string.cnpg_restart_primary, R.string.cnpg_restart_risk, KubeAction.CNPG_RESTART_PRIMARY),
    HIBERNATE(R.string.cnpg_hibernate_action, R.string.cnpg_hibernate_risk, KubeAction.CNPG_HIBERNATE),
    WAKE(R.string.cnpg_wake_action, R.string.cnpg_wake_risk, KubeAction.CNPG_HIBERNATE),
}
data class CnpgLifecycleResult(val label: String, val error: UiText?)

@Composable
fun CnpgLifecycleToasts(results: Flow<CnpgLifecycleResult>) = ResultToasts(results) { context, r ->
    (r.error?.resolve(context)?.let { context.getString(R.string.cnpg_lifecycle_failed, it) }
        ?: context.getString(R.string.cnpg_lifecycle_requested)) to (r.error != null)
}

@Composable
fun CnpgLifecycleButtons(cluster: CnpgCluster, busy: Boolean, onAction: (CnpgLifecycleAction) -> Unit) {
    val actions = if (cluster.hibernated) listOf(CnpgLifecycleAction.WAKE)
        else listOfNotNull(CnpgLifecycleAction.RESTART.takeIf { cluster.currentPrimary.isNotEmpty() }, CnpgLifecycleAction.HIBERNATE)
    actions.forEach { action ->
        val denial = rememberKubeDenial(action.permission, cluster.namespace)
        IchorOutlinedButton(onClick = { onAction(action) }, enabled = !busy && denial == null) { Text(stringResource(action.title)) }
        denial?.let { KubeDenialNote(it) }
    }
}

@Composable
fun CnpgLifecycleConfirmDialog(cluster: CnpgCluster, action: CnpgLifecycleAction, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    val denial = rememberKubeDenial(action.permission, cluster.namespace)
    HostnameConfirmDialog(title = stringResource(action.title), hostname = cluster.name,
        confirmLabel = stringResource(action.title), enabled = denial == null, onConfirm = onConfirm, onDismiss = onDismiss) {
        Text(stringResource(action.risk))
        if (action == CnpgLifecycleAction.RESTART) Text(stringResource(R.string.cnpg_restart_target, cluster.currentPrimary))
        denial?.let { KubeDenialNote(it) }
    }
}
