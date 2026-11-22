package name.levis.ichor.ui.storage

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.DiskLayout
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** What wiping [disk] (its kernel name) did, shown once. */
data class DiskWipeResult(val disk: String, val error: UiText?)

/**
 * Wiping one disk from the Storage screen: the node's [hostname] typed to confirm, the disks
 * being wiped ([busy], by name), and [onWipe]. Null on a node or role that may not wipe.
 */
data class DiskWipeControls(val hostname: String, val busy: Set<String>, val onWipe: (DiskLayout) -> Unit)

/**
 * "Wipe this disk…", then the disk named (path, model, serial, size) with what is erased and
 * what Talos cannot know, behind the node's typed hostname. Irreversible.
 */
@Composable
fun DiskWipeButton(disk: DiskLayout, controls: DiskWipeControls) {
    var confirming by remember { mutableStateOf(false) }
    val busy = disk.name in controls.busy
    IchorOutlinedButton(onClick = { confirming = true }, enabled = !busy) {
        Text(stringResource(if (busy) R.string.storage_disk_wiping else R.string.storage_disk_wipe))
    }
    if (confirming) {
        HostnameConfirmDialog(
            title = stringResource(R.string.storage_disk_wipe_title, disk.devPath),
            hostname = controls.hostname,
            confirmLabel = stringResource(R.string.storage_disk_wipe_confirm),
            emphasized = true,
            onConfirm = {
                confirming = false
                controls.onWipe(disk)
            },
            onDismiss = { confirming = false },
        ) {
            Text(
                listOf(disk.devPath, disk.model, disk.serial, formatBytes(disk.size)).filter { it.isNotBlank() }.joinToString("  ·  "),
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(stringResource(R.string.storage_disk_wipe_text), style = MaterialTheme.typography.bodyMedium, color = LocalStatusColors.current.bad)
            Text(stringResource(R.string.storage_disk_wipe_storage_note), style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
fun DiskWipeToasts(results: Flow<DiskWipeResult>) = ResultToasts(results) { context, r ->
    val message = r.error?.resolve(context)?.let { context.getString(R.string.storage_disk_wipe_failed, r.disk, it) }
        ?: context.getString(R.string.storage_disk_wipe_done, r.disk)
    message to (r.error != null)
}
