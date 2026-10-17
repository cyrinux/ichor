package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.selection.selectable
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.LonghornEngineImage
import name.levis.ichor.model.LonghornEngineUpgradePreview
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** Outcome of an engine upgrade, shown once. */
data class LonghornEngineResult(val error: UiText?)

@Composable
fun LonghornEngineToasts(results: Flow<LonghornEngineResult>) = ResultToasts(results) { context, r ->
    (r.error?.resolve(context)?.let { context.getString(R.string.longhorn_engine_failed, it) } ?: context.getString(R.string.longhorn_engine_requested)) to (r.error != null)
}

/**
 * Upgrades [volume]'s engine: the current image, the deployed ones to pick from (the default
 * preselected), live or offline, Longhorn's reason when it refuses, the volume name typed to confirm.
 */
@Composable
fun LonghornEngineDialog(volume: LonghornVolume, actions: LonghornActions, onDismiss: () -> Unit, onConfirm: (String) -> Unit) {
    var preview by remember(volume.namespace, volume.name) { mutableStateOf<LonghornEngineUpgradePreview?>(null) }
    var selected by remember(volume.namespace, volume.name) { mutableStateOf("") }
    var error by remember(volume.namespace, volume.name) { mutableStateOf<UiText?>(null) }
    val denial = rememberKubeDenial(KubeAction.LONGHORN_ENGINE_UPGRADE, volume.namespace)
    LaunchedEffect(volume.namespace, volume.name) {
        try {
            val p = actions.enginePreview(volume)
            preview = p
            selected = p.preselected
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e.uiText()
        }
    }
    val title = stringResource(R.string.longhorn_engine_upgrade)
    HostnameConfirmDialog(
        title = title,
        hostname = volume.name,
        confirmLabel = title,
        enabled = preview?.accepts(selected) == true && denial == null,
        onConfirm = { onConfirm(selected) },
        onDismiss = onDismiss,
    ) {
        error?.let { Text(it.asString()) }
        if (preview == null && error == null) CircularProgressIndicator()
        preview?.let { p -> EnginePreviewContent(p, selected, onSelect = { selected = it }) }
        denial?.let { KubeDenialNote(it) }
    }
}

@Composable
private fun EnginePreviewContent(p: LonghornEngineUpgradePreview, selected: String, onSelect: (String) -> Unit) {
    val colors = LocalStatusColors.current
    if (p.error.isNotEmpty()) Text(p.error, color = colors.bad)
    Column {
        MutedText(stringResource(R.string.longhorn_engine_current))
        Text(p.currentImage.ifEmpty { "—" }, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
    }
    Text(stringResource(if (p.live) R.string.longhorn_engine_live else R.string.longhorn_engine_offline), color = colors.warn)
    if (!p.canUpgrade && p.reason.isNotEmpty()) Text(p.reason, color = colors.bad)
    val choices = p.images.filter { it.deployed && it.image != p.currentImage }
    if (choices.isNotEmpty()) {
        MutedText(stringResource(R.string.longhorn_engine_image))
        Column {
            choices.forEach { image -> EngineImageRow(image, selected == image.image, enabled = p.canUpgrade) { onSelect(image.image) } }
        }
    }
}

@Composable
private fun EngineImageRow(image: LonghornEngineImage, isSelected: Boolean, enabled: Boolean, onClick: () -> Unit) {
    val usable = enabled && image.selectable
    Row(
        Modifier.fillMaxWidth().selectable(selected = isSelected, enabled = usable, role = Role.RadioButton, onClick = onClick),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = isSelected, onClick = null, enabled = usable)
        Column {
            Text(image.version.ifEmpty { image.image }, style = MaterialTheme.typography.bodyMedium)
            val notes = listOfNotNull(
                image.image.takeIf { image.version.isNotEmpty() },
                stringResource(R.string.longhorn_engine_default).takeIf { image.default },
                stringResource(R.string.longhorn_engine_incompatible).takeIf { image.incompatible },
            )
            if (notes.isNotEmpty()) MutedText(notes.joinToString(" · "))
        }
    }
}
