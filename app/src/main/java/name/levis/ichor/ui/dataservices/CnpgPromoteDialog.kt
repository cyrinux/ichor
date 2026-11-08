package name.levis.ichor.ui.dataservices

import androidx.compose.material3.CircularProgressIndicator
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.CnpgCluster
import name.levis.ichor.model.CnpgPromotePreview
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.promotablePods
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.formatBytes

data class CnpgPromoteResult(val label: String, val instance: String, val error: UiText?)

@Composable
fun CnpgPromoteToasts(results: Flow<CnpgPromoteResult>) = ResultToasts(results) { context, r ->
    val message = r.error?.resolve(context)?.let { context.getString(R.string.cnpg_promote_failed, r.label, it) }
        ?: context.getString(R.string.cnpg_promote_requested, r.instance)
    message to (r.error != null)
}

@Composable
fun CnpgPromoteButtons(cluster: CnpgCluster, busy: Boolean, onPromote: (String) -> Unit) {
    val denial = rememberKubeDenial(KubeAction.CNPG_PROMOTE, cluster.namespace)
    cluster.promotablePods.forEach { pod ->
        IchorOutlinedButton(onClick = { onPromote(pod.name) }, enabled = !busy && denial == null) {
            Text(stringResource(R.string.cnpg_promote_instance, pod.name))
        }
    }
    if (cluster.promotablePods.isNotEmpty()) denial?.let { KubeDenialNote(it) }
}

@Composable
fun CnpgPromoteConfirmDialog(cluster: CnpgCluster, instance: String, actions: CnpgActions, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    var preview by remember(cluster.label, instance) { mutableStateOf<CnpgPromotePreview?>(null) }
    var error by remember(cluster.label, instance) { mutableStateOf<UiText?>(null) }
    val denial = rememberKubeDenial(KubeAction.CNPG_PROMOTE, cluster.namespace)
    LaunchedEffect(cluster.label, instance) {
        try { preview = actions.preview(cluster, instance) }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { error = e.uiText() }
    }
    HostnameConfirmDialog(title = stringResource(R.string.cnpg_promote_title), hostname = cluster.name,
        confirmLabel = stringResource(R.string.cnpg_promote_title), enabled = preview?.canPromote == true && denial == null,
        onConfirm = onConfirm, onDismiss = onDismiss) {
        Text(stringResource(R.string.cnpg_promote_risk))
        error?.let { Text(it.asString()) }
        if (preview == null && error == null) CircularProgressIndicator()
        preview?.let { p ->
            Text(stringResource(R.string.cnpg_promote_primary, p.currentPrimary))
            Text(stringResource(R.string.cnpg_promote_target, p.instance))
            Text(stringResource(R.string.cnpg_promote_lag, p.lagBytes?.let(::formatBytes) ?: stringResource(R.string.cnpg_lag_unknown)))
            if ("lagNonzero" in p.warnings) Text(stringResource(R.string.cnpg_lag_warning))
            if ("lagUnknown" in p.warnings) Text(stringResource(R.string.cnpg_lag_unknown_warning))
            if (!p.canPromote) Text(stringResource(R.string.cnpg_promote_unavailable))
            if (p.lagError.isNotEmpty()) Text(p.lagError)
        }
        denial?.let { KubeDenialNote(it) }
    }
}
