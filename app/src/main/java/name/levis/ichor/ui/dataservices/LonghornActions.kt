package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedIconButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.longhornActionKey
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.LonghornSnapshotAction
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.model.kubeAction
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubePermission
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeActionAccess
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** Outcome of a Longhorn action on [label] (a volume's claim or a node), shown once. */
data class LonghornActionResult(val action: LonghornAction, val label: String, val value: Int, val error: UiText?)

/**
 * Longhorn volume and node actions run in [scope] (a ViewModel's). [onChanged] runs after an
 * action went through, to refresh the data services and show its effect.
 */
class LonghornActions(
    private val scope: CoroutineScope,
    private val dataServices: DataServicesRepository,
    private val onChanged: () -> Unit,
) {
    private val attachment = KeyedActions<LonghornAttachmentResult>(scope)
    val attachmentBusy: StateFlow<Set<String>> get() = attachment.busy
    val attachmentResults: Flow<LonghornAttachmentResult> get() = attachment.results
    suspend fun attachmentPreview(volume: name.levis.ichor.model.LonghornVolume) = dataServices.longhornAttachmentPreview(volume.namespace, volume.name)
    fun attachment(volume: name.levis.ichor.model.LonghornVolume, node: String, attach: Boolean) {
        val key = longhornActionKey(volume.namespace, volume.name)
        if (inFlight(key)) return
        attachment.launch(longhornActionKey(volume.namespace, volume.name), { dataServices.longhornAttachment(volume.namespace, volume.name, node, attach) }, {
            LonghornAttachmentResult(it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
    private val engine = KeyedActions<LonghornEngineResult>(scope)
    val engineBusy: StateFlow<Set<String>> get() = engine.busy
    val engineResults: Flow<LonghornEngineResult> get() = engine.results
    suspend fun enginePreview(volume: name.levis.ichor.model.LonghornVolume) = dataServices.longhornEngineUpgradePreview(volume.namespace, volume.name)
    fun engineUpgrade(volume: name.levis.ichor.model.LonghornVolume, image: String) {
        val key = longhornActionKey(volume.namespace, volume.name)
        if (inFlight(key)) return
        engine.launch(key, { dataServices.longhornEngineUpgrade(volume.namespace, volume.name, image) }, {
            LonghornEngineResult(it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
    /** Another volume action runs on [key]: one at a time, whichever its kind. */
    private fun inFlight(key: String) =
        key in busy.value || key in salvageBusy.value || key in attachmentBusy.value || key in engineBusy.value
    private val salvage = KeyedActions<LonghornSalvageResult>(scope)
    val salvageBusy: StateFlow<Set<String>> get() = salvage.busy
    val salvageResults: Flow<LonghornSalvageResult> get() = salvage.results
    suspend fun salvagePreview(volume: name.levis.ichor.model.LonghornVolume) = dataServices.longhornSalvagePreview(volume.namespace, volume.name)
    fun salvage(volume: name.levis.ichor.model.LonghornVolume, replicas: Set<String>) {
        val key = longhornActionKey(volume.namespace, volume.name)
        if (inFlight(key)) return
        salvage.launch(longhornActionKey(volume.namespace, volume.name), { dataServices.longhornSalvage(volume.namespace, volume.name, replicas) }, {
            LonghornSalvageResult(volume.label, it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
    private val snapshot = KeyedActions<LonghornSnapshotResult>(scope)
    /** Keys (longhornSnapshotKey) of the snapshots, or volumes taking one, with an action in flight. */
    val snapshotBusy: StateFlow<Set<String>> get() = snapshot.busy
    val snapshotResults: Flow<LonghornSnapshotResult> get() = snapshot.results
    /** Runs [action] on [snapshotName] of [volume], "" for [LonghornSnapshotAction.TAKE]. */
    fun snapshot(volume: name.levis.ichor.model.LonghornVolume, snapshotName: String, action: LonghornSnapshotAction) {
        snapshot.launch(longhornSnapshotKey(volume, snapshotName), { dataServices.longhornSnapshotAction(volume.namespace, volume.name, snapshotName, action) }, {
            LonghornSnapshotResult(action, volume.label, it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
    private val actions = KeyedActions<LonghornActionResult>(scope)
    /** Keys (longhornActionKey) of the volumes and nodes with an action in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy

    val results: Flow<LonghornActionResult> get() = actions.results

    fun run(key: String, namespace: String, name: String, label: String, action: LonghornAction, value: Int = 0) {
        if (inFlight(key)) return
        actions.launch(key, { dataServices.longhornAction(namespace, name, action, value) }, {
            LonghornActionResult(action, label, value, it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
}

/** Outcome of a snapshot action on the volume [label], shown once. */
data class LonghornSnapshotResult(val action: LonghornSnapshotAction, val label: String, val error: UiText?)

/** Busy key of [snapshotName] of [volume]; "" is the volume taking a new one. */
fun longhornSnapshotKey(volume: name.levis.ichor.model.LonghornVolume, snapshotName: String) =
    longhornActionKey(volume.namespace, volume.name) + "@" + snapshotName

/** A toast for each snapshot outcome of [results]. */
@Composable
fun LonghornSnapshotToasts(results: Flow<LonghornSnapshotResult>) = ResultToasts(results) { context, r ->
    val text = r.error?.resolve(context)?.let { context.getString(R.string.longhorn_action_failed, r.label, it) }
        ?: when (r.action) {
            LonghornSnapshotAction.TAKE -> context.getString(R.string.longhorn_snapshot_taken, r.label)
            LonghornSnapshotAction.BACKUP -> context.getString(R.string.longhorn_backup_started, r.label)
            LonghornSnapshotAction.DELETE -> context.getString(R.string.longhorn_snapshot_deleted, r.label)
        }
    text to (r.error != null)
}

/** A toast for each outcome of [results]. */
@Composable
fun LonghornResultToasts(results: Flow<LonghornActionResult>) = ResultToasts(results) { context, r ->
    val text = r.error?.resolve(context)?.let { context.getString(R.string.longhorn_action_failed, r.label, it) }
        ?: when (r.action) {
            LonghornAction.BACKUP -> context.getString(R.string.longhorn_backup_started, r.label)
            LonghornAction.TRIM -> context.getString(R.string.longhorn_trim_started, r.label)
            LonghornAction.REPLICAS -> context.getString(R.string.longhorn_replicas_set, r.label, r.value)
            LonghornAction.SCHEDULING_ON -> context.getString(R.string.longhorn_scheduling_on_done, r.label)
            LonghornAction.SCHEDULING_OFF -> context.getString(R.string.longhorn_scheduling_off_done, r.label)
            LonghornAction.EVICT -> context.getString(R.string.longhorn_evict_started, r.label)
            LonghornAction.CANCEL_EVICTION -> context.getString(R.string.longhorn_evict_cancelled, r.label)
        }
    text to (r.error != null)
}

/**
 * A menu item outside [LonghornAction]: salvage, attach or detach, the snapshots sheet. Its own
 * dialog or sheet confirms it; [action] is what the account must be allowed, null for none.
 */
internal data class LonghornMenuExtra(val label: String, val action: KubeAction?, val onClick: () -> Unit)

/**
 * The ⋮ menu of a volume or node row, a spinner while one of its actions runs: [extras] first,
 * then [actions]. An action the account may not run in [namespace] (Longhorn's own) is
 * disabled, with the reason under it.
 */
@Composable
internal fun LonghornActionMenu(
    actions: List<LonghornAction>,
    namespace: String,
    busy: Boolean,
    onAction: (LonghornAction) -> Unit,
    extras: List<LonghornMenuExtra> = emptyList(),
) {
    if (busy) {
        Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        }
        return
    }
    var open by remember { mutableStateOf(false) }
    val access = rememberKubeActionAccess(namespace)
    // The Box anchors the menu to the button, not to the row's start edge.
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            extras.forEach { extra ->
                MenuItem(extra.label, access?.denial(extra.action)) {
                    open = false
                    extra.onClick()
                }
            }
            actions.forEach { action ->
                MenuItem(stringResource(action.menuLabel), access?.denial(action.kubeAction)) {
                    open = false
                    onAction(action)
                }
            }
        }
    }
}

@Composable
private fun MenuItem(label: String, denial: KubePermission?, onClick: () -> Unit) {
    DropdownMenuItem(
        text = {
            Column {
                Text(label)
                KubeDenialNote(denial)
            }
        },
        enabled = denial == null,
        onClick = onClick,
    )
}

private val LonghornAction.menuLabel: Int
    get() = when (this) {
        LonghornAction.BACKUP -> R.string.longhorn_action_backup
        LonghornAction.TRIM -> R.string.longhorn_action_trim
        LonghornAction.REPLICAS -> R.string.longhorn_action_replicas
        LonghornAction.SCHEDULING_ON -> R.string.longhorn_action_scheduling_on
        LonghornAction.SCHEDULING_OFF -> R.string.longhorn_action_scheduling_off
        LonghornAction.EVICT -> R.string.longhorn_action_evict
        LonghornAction.CANCEL_EVICTION -> R.string.longhorn_action_cancel_eviction
    }

/** Picks a volume's replica count in [choices], starting from [current]. */
@Composable
fun ReplicaCountDialog(label: String, current: Int, choices: IntRange, onConfirm: (Int) -> Unit, onDismiss: () -> Unit) {
    var count by remember { mutableIntStateOf(current.coerceIn(choices)) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.longhorn_replicas_title, label)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(stringResource(R.string.longhorn_replicas_text))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                    OutlinedIconButton(onClick = { count-- }, enabled = count > choices.first) { Text("−") }
                    Text("$count", style = MaterialTheme.typography.headlineSmall, textAlign = TextAlign.Center, modifier = Modifier.width(64.dp))
                    OutlinedIconButton(onClick = { count++ }, enabled = count < choices.last) { Text("+") }
                }
                if (count < current) {
                    Text(stringResource(R.string.longhorn_replicas_fewer), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn)
                }
            }
        },
        confirmButton = { TextButton(onClick = { onConfirm(count) }, enabled = count != current) { Text(stringResource(R.string.longhorn_replicas_confirm)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** Confirms an eviction: every replica on [node] is copied elsewhere, then removed. */
@Composable
fun EvictConfirmDialog(node: String, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.longhorn_evict_title, node),
        text = stringResource(R.string.longhorn_evict_text, node),
        confirm = stringResource(R.string.longhorn_action_evict),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
        confirmColor = LocalStatusColors.current.warn,
    )
}
