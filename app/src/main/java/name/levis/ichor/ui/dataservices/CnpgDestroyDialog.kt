package name.levis.ichor.ui.dataservices

import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.CnpgCluster
import name.levis.ichor.model.CnpgDestroyPreview
import name.levis.ichor.model.CnpgDestroyVolume
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.removableInstances
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiText

data class CnpgDestroyResult(val label: String, val instance: String, val error: UiText?)

@Composable
fun CnpgDestroyToasts(results: Flow<CnpgDestroyResult>) = ResultToasts(results) { context, r ->
    val message = r.error?.resolve(context)?.let { context.getString(R.string.cnpg_destroy_failed, r.instance, it) }
        ?: context.getString(R.string.cnpg_destroy_done, r.instance)
    message to (r.error != null)
}

/** A "Remove" button per failed, pod-less or unscheduled instance (never the primary). */
@Composable
fun CnpgDestroyButtons(cluster: CnpgCluster, busy: Boolean, onDestroy: (String) -> Unit) {
    val instances = cluster.removableInstances
    if (instances.isEmpty()) return
    val denial = rememberKubeDenial(KubeAction.CNPG_DESTROY, cluster.namespace)
    instances.forEach { instance ->
        OutlinedButton(onClick = { onDestroy(instance) }, enabled = !busy && denial == null) {
            Text(stringResource(R.string.cnpg_destroy_instance, instance))
        }
    }
    denial?.let { KubeDenialNote(it) }
}

/** Confirms removing [instance] after a fresh preview: where its volumes live, what blocks it. */
@Composable
fun CnpgDestroyConfirmDialog(cluster: CnpgCluster, instance: String, actions: CnpgActions, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    var preview by remember(cluster.label, instance) { mutableStateOf<CnpgDestroyPreview?>(null) }
    var error by remember(cluster.label, instance) { mutableStateOf<UiText?>(null) }
    val denial = rememberKubeDenial(KubeAction.CNPG_DESTROY, cluster.namespace)
    LaunchedEffect(cluster.label, instance) {
        try { preview = actions.destroyPreview(cluster, instance) }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { error = e.uiText() }
    }
    HostnameConfirmDialog(title = stringResource(R.string.cnpg_destroy_title), hostname = instance,
        confirmLabel = stringResource(R.string.cnpg_destroy_title), enabled = preview?.canDestroy == true && denial == null,
        onConfirm = onConfirm, onDismiss = onDismiss) {
        Text(stringResource(R.string.cnpg_destroy_risk))
        error?.let { Text(it.asString()) }
        if (preview == null && error == null) CircularProgressIndicator()
        preview?.let { p -> DestroyPreview(p) }
        denial?.let { KubeDenialNote(it) }
    }
}

@Composable
private fun DestroyPreview(p: CnpgDestroyPreview) {
    if (p.currentPrimary.isNotEmpty()) Text(stringResource(R.string.cnpg_promote_primary, p.currentPrimary))
    Text(
        if (p.podPhase.isEmpty()) stringResource(R.string.cnpg_destroy_no_pod)
        else stringResource(R.string.cnpg_destroy_pod, listOf(p.podPhase, p.podNode).filter { it.isNotEmpty() }.joinToString(" · ")),
    )
    p.volumes.forEach { Text("${it.name} · ${volumeWhere(it)}") }
    if ("primary" in p.blockers) Text(stringResource(R.string.cnpg_destroy_block_primary))
    if ("noOtherData" in p.blockers) Text(stringResource(R.string.cnpg_destroy_block_no_other_data))
    if ("notFound" in p.blockers) Text(stringResource(R.string.cnpg_destroy_block_not_found))
    if ("ready" in p.warnings) Text(stringResource(R.string.cnpg_destroy_warn_ready))
    if ("nodePresent" in p.warnings) Text(stringResource(R.string.cnpg_destroy_warn_node_present))
}

@Composable
private fun volumeWhere(v: CnpgDestroyVolume): String = when (v.nodeState) {
    "gone" -> stringResource(R.string.cnpg_destroy_node_gone, v.node)
    "present" -> stringResource(R.string.cnpg_destroy_node_present, v.node)
    "none" -> stringResource(R.string.cnpg_destroy_node_none)
    else -> stringResource(R.string.cnpg_destroy_node_unknown)
}
