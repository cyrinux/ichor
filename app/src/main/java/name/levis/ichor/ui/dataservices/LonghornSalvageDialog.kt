package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.LonghornSalvagePreview
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiText

data class LonghornSalvageResult(val label: String, val error: UiText?)
@Composable
fun LonghornSalvageToasts(results: Flow<LonghornSalvageResult>) = ResultToasts(results) { context, r ->
    (r.error?.resolve(context)?.let { context.getString(R.string.longhorn_salvage_failed, it) } ?: context.getString(R.string.longhorn_salvage_requested)) to (r.error != null)
}
@Composable
fun LonghornSalvageButton(volume: LonghornVolume, busy: Boolean, onClick: () -> Unit) {
    if (volume.robustness != "faulted") return
    val denial = rememberKubeDenial(KubeAction.LONGHORN_SALVAGE, volume.namespace)
    OutlinedButton(onClick = onClick, enabled = !busy && denial == null) { Text(stringResource(R.string.longhorn_salvage)) }
    denial?.let { KubeDenialNote(it) }
}
@Composable
fun LonghornSalvageDialog(volume: LonghornVolume, actions: LonghornActions, onDismiss: () -> Unit, onConfirm: (Set<String>) -> Unit) {
    var preview by remember(volume.name) { mutableStateOf<LonghornSalvagePreview?>(null) }
    var selection by remember(volume.name) { mutableStateOf(emptySet<String>()) }
    var error by remember(volume.name) { mutableStateOf<UiText?>(null) }
    val denial = rememberKubeDenial(KubeAction.LONGHORN_SALVAGE, volume.namespace)
    LaunchedEffect(volume.namespace, volume.name) {
        try { preview = actions.salvagePreview(volume) }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { error = e.uiText() }
    }
    HostnameConfirmDialog(title = stringResource(R.string.longhorn_salvage), hostname = volume.name,
        confirmLabel = stringResource(R.string.longhorn_salvage), enabled = preview?.accepts(selection) == true && denial == null,
        onConfirm = { onConfirm(selection) }, onDismiss = onDismiss) {
        Text(stringResource(R.string.longhorn_salvage_risk))
        Text(stringResource(R.string.longhorn_salvage_choose))
        error?.let { Text(it.asString()) }
        if (preview == null && error == null) CircularProgressIndicator()
        preview?.let { p ->
            Text("${p.state} · ${p.robustness}")
            if (!p.canSalvage) Text(stringResource(R.string.longhorn_salvage_unavailable))
            if (p.error.isNotEmpty()) Text(p.error)
            p.replicas.forEach { replica ->
                Row {
                    Checkbox(checked = replica.name in selection, enabled = replica.eligible,
                        onCheckedChange = { checked -> selection = if (checked) selection + replica.name else selection - replica.name })
                    Text(stringResource(R.string.longhorn_salvage_replica, replica.name, replica.node, agoOrNever(replica.healthyAt), agoOrNever(replica.failedAt)))
                }
            }
        }
        denial?.let { KubeDenialNote(it) }
    }
}
