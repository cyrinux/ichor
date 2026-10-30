package name.levis.ichor.ui.dataservices
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.*
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.*
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiText

data class LonghornAttachmentResult(val error: UiText?)
@Composable
fun LonghornAttachmentToasts(results: Flow<LonghornAttachmentResult>) = ResultToasts(results) { context, r ->
    (r.error?.resolve(context)?.let { context.getString(R.string.longhorn_attachment_failed, it) } ?: context.getString(R.string.longhorn_attachment_requested)) to (r.error != null)
}
/** What the row's menu offers: true to attach a detached volume, false to detach, null for neither. */
val LonghornVolume.attachmentChoice: Boolean?
    get() = when {
        state == "attached" -> false
        state == "detached" && robustness != "faulted" -> true
        else -> null
    }
@Composable
fun LonghornAttachmentDialog(volume: LonghornVolume, attach: Boolean, actions: LonghornActions, onDismiss: () -> Unit, onConfirm: (String) -> Unit) {
    var preview by remember(volume.namespace, volume.name) { mutableStateOf<LonghornAttachmentPreview?>(null) }
    var selected by remember(volume.namespace, volume.name) { mutableStateOf("") }
    var error by remember(volume.namespace, volume.name) { mutableStateOf<UiText?>(null) }
    val denial = rememberKubeDenial(KubeAction.LONGHORN_ATTACHMENT, volume.namespace)
    LaunchedEffect(volume.namespace, volume.name) {
        try { preview = actions.attachmentPreview(volume); if (!attach) selected = preview?.node.orEmpty() }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { error = e.uiText() }
    }
    val title = stringResource(if (attach) R.string.longhorn_attach else R.string.longhorn_detach)
    HostnameConfirmDialog(title = title, hostname = volume.name, confirmLabel = title,
        enabled = preview?.accepts(selected, attach) == true && denial == null, onConfirm = { onConfirm(selected) }, onDismiss = onDismiss) {
        Text(stringResource(R.string.longhorn_attachment_risk))
        error?.let { Text(it.asString()) }
        if (preview == null && error == null) CircularProgressIndicator()
        preview?.let { p ->
            if (p.error.isNotEmpty()) Text(p.error)
            if (!(if (attach) p.canAttach else p.canDetach)) Text(stringResource(R.string.longhorn_attachment_unavailable))
            if (attach) {
                Text(stringResource(R.string.longhorn_attachment_choose))
                p.nodes.forEach { node -> Row {
                    RadioButton(selected = selected == node.name, enabled = node.eligible, onClick = { selected = node.name })
                    Text(node.name)
                } }
            } else Text(stringResource(R.string.longhorn_attachment_current, p.node))
        }
        denial?.let { KubeDenialNote(it) }
    }
}
