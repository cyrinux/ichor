package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import name.levis.ichor.ui.design.IchorAlertDialog
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.R
import name.levis.ichor.data.AutoscalingRepository
import name.levis.ichor.model.HpaRow
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.MAX_SCALE_REPLICAS
import name.levis.ichor.model.ManagedNodeGroupRow
import name.levis.ichor.model.NODE_GROUP_UPDATE_UNREADABLE
import name.levis.ichor.model.NodeGroupIssue
import name.levis.ichor.model.NodeGroupUpdate
import name.levis.ichor.model.SCALED_OBJECT_HOLD
import name.levis.ichor.model.SCALED_OBJECT_RESUME
import name.levis.ichor.model.ScaledObjectRow
import name.levis.ichor.model.kedaOwned
import name.levis.ichor.model.scaleNeedsTypedName
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiText

/** What an autoscaling action did. */
enum class AutoscalingAction { PAUSE, HOLD, RESUME, BOUNDS, GROUP_SIZE, GROUP_UPGRADE, CONTROL_PLANE_UPGRADE }

/** How often an open node group sheet reads the update AWS runs. */
private const val NODE_GROUP_POLL_MS = 10_000L

/** How often the open control plane sheet reads its upgrade (20 to 40 minutes in all). */
private const val CONTROL_PLANE_POLL_MS = 30_000L

/** The busy key of the control plane upgrade. */
const val EKS_CONTROL_PLANE_KEY = "eks/control-plane"

/** Outcome of [action] on the object [name], shown once. */
data class AutoscalingActionResult(val name: String, val action: AutoscalingAction, val error: UiText?)

/** Keys of the rows with an action in flight. */
val HpaRow.actionKey: String get() = "hpa/$key"
val ScaledObjectRow.actionKey: String get() = "so/$key"
val ManagedNodeGroupRow.actionKey: String get() = "ng/$key"

/** Pause, hold and resume a ScaledObject, and set an HPA's bounds, run in [scope] (a ViewModel's); [onChanged] reads the report again after one. */
class AutoscalingActions(
    scope: CoroutineScope,
    private val autoscaling: AutoscalingRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<AutoscalingActionResult>(scope)
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<AutoscalingActionResult> get() = actions.results

    private val _groupUpdates = MutableStateFlow<Map<String, NodeGroupUpdate>>(emptyMap())

    /** The last update each node group's size change or upgrade started, by group name, as last read. */
    val groupUpdates: StateFlow<Map<String, NodeGroupUpdate>> = _groupUpdates.asStateFlow()

    /** [replicas] ≥ 0 pauses at that count; [SCALED_OBJECT_HOLD] holds, [SCALED_OBJECT_RESUME] resumes. */
    fun pause(row: ScaledObjectRow, replicas: Int) {
        val action = when (replicas) {
            SCALED_OBJECT_HOLD -> AutoscalingAction.HOLD
            SCALED_OBJECT_RESUME -> AutoscalingAction.RESUME
            else -> AutoscalingAction.PAUSE
        }
        actions.launch(
            row.actionKey,
            { autoscaling.pauseScaledObject(row.namespace, row.name, replicas) },
            { AutoscalingActionResult(row.name, action, it.exceptionOrNull()?.uiText()) },
        ) { onChanged() }
    }

    fun setBounds(row: HpaRow, min: Int, max: Int) {
        actions.launch(
            row.actionKey,
            { autoscaling.setHpaBounds(row.namespace, row.name, min, max) },
            { AutoscalingActionResult(row.name, AutoscalingAction.BOUNDS, it.exceptionOrNull()?.uiText()) },
        ) { onChanged() }
    }

    /** Changes the node group's size; the report read again shows it updating. Never retried. */
    fun scaleNodeGroup(row: ManagedNodeGroupRow, desired: Int, min: Int, max: Int) {
        actions.launch(
            row.actionKey,
            {
                autoscaling.scaleNodeGroup(row.name, desired, min, max).also { u -> _groupUpdates.update { it + (row.name to u) } }
            },
            { AutoscalingActionResult(row.name, AutoscalingAction.GROUP_SIZE, it.exceptionOrNull()?.uiText()) },
        ) { onChanged() }
    }

    /** Upgrades the node group ([version] empty: the latest node image of its own); followed like a size change. Never retried. */
    fun upgradeNodeGroup(row: ManagedNodeGroupRow, version: String, force: Boolean) {
        actions.launch(
            row.actionKey,
            {
                autoscaling.nodeGroupUpgrade(row.name, version, force).also { u -> _groupUpdates.update { it + (row.name to u) } }
            },
            { AutoscalingActionResult(row.name, AutoscalingAction.GROUP_UPGRADE, it.exceptionOrNull()?.uiText()) },
        ) { onChanged() }
    }

    private val _controlPlaneUpdate = MutableStateFlow<NodeGroupUpdate?>(null)

    /** The control plane upgrade this screen started or found running, as last read; null before one. */
    val controlPlaneUpdate: StateFlow<NodeGroupUpdate?> = _controlPlaneUpdate.asStateFlow()

    /** Upgrades the EKS control plane to [version] (the plan's next minor). Cannot be undone; never retried. */
    fun upgradeControlPlane(cluster: String, version: String) {
        actions.launch(
            EKS_CONTROL_PLANE_KEY,
            { autoscaling.eksUpgrade(version).also { _controlPlaneUpdate.value = it } },
            { AutoscalingActionResult(cluster, AutoscalingAction.CONTROL_PLANE_UPGRADE, it.exceptionOrNull()?.uiText()) },
        ) { onChanged() }
    }

    /**
     * Reads the control plane's update every 30 s while it is in progress and the caller (the
     * open sheet) runs, starting from [found] (the plan's, when the app was reopened) unless this
     * screen already follows one. A failed read is a moment the API is unreachable during the
     * upgrade: it stays in progress and is read again. Its end reads the report again.
     */
    suspend fun followControlPlaneUpdate(found: NodeGroupUpdate?) {
        if (_controlPlaneUpdate.value == null && found != null) _controlPlaneUpdate.value = found
        while (true) {
            val current = _controlPlaneUpdate.value?.takeIf { it.inProgress } ?: return
            delay(CONTROL_PLANE_POLL_MS)
            val next = cancellableCatching { autoscaling.eksUpdate(current.id) }.getOrNull() ?: continue
            _controlPlaneUpdate.value = next
            if (!next.inProgress) {
                onChanged()
                return
            }
        }
    }

    /**
     * Reads [group]'s update every 10 s while it is in progress and the caller (the open sheet)
     * runs; its end reads the report again. A failed read ends it with the reason.
     */
    suspend fun followGroupUpdate(group: String) {
        while (true) {
            val current = _groupUpdates.value[group]?.takeIf { it.inProgress } ?: return
            delay(NODE_GROUP_POLL_MS)
            val read = cancellableCatching { autoscaling.nodeGroupUpdate(group, current.id) }
            val next = read.getOrElse { e ->
                current.copy(status = NODE_GROUP_UPDATE_UNREADABLE, errors = listOf(NodeGroupIssue(message = e.message.orEmpty())))
            }
            _groupUpdates.update { it + (group to next) }
            if (!next.inProgress) {
                onChanged()
                return
            }
        }
    }
}

@Composable
fun AutoscalingActionToasts(results: Flow<AutoscalingActionResult>) = ResultToasts(results) { context, r ->
    val message = r.error?.resolve(context)?.let { context.getString(R.string.autoscaling_action_failed, r.name, it) }
        ?: context.getString(
            when (r.action) {
                AutoscalingAction.PAUSE -> R.string.autoscaling_pause_done
                AutoscalingAction.HOLD -> R.string.autoscaling_hold_done
                AutoscalingAction.RESUME -> R.string.autoscaling_resume_done
                AutoscalingAction.BOUNDS -> R.string.autoscaling_bounds_done
                AutoscalingAction.GROUP_SIZE -> R.string.autoscaling_nodegroup_scale_started
                AutoscalingAction.GROUP_UPGRADE, AutoscalingAction.CONTROL_PLANE_UPGRADE -> R.string.autoscaling_nodegroup_upgrade_started
            },
            r.name,
        )
    message to (r.error != null)
}

/**
 * Pause at a count, hold where it is, or resume (when paused): the buttons are disabled with
 * the reason when the credentials may not patch ScaledObjects here. Reversible: a plain dialog,
 * but a pause at 0 stops every pod, so it asks for the typed name like a scale to 0.
 */
@Composable
fun ScaledObjectPauseSection(row: ScaledObjectRow, busy: Boolean, onPause: (Int) -> Unit) {
    val denial = rememberKubeDenial(KubeAction.AUTOSCALE_KEDA, row.namespace)
    var picking by remember { mutableStateOf(false) }
    var resuming by remember { mutableStateOf(false) }
    // A pause at 0 stops every pod, like a scale to 0: the same typed name.
    var confirmZero by remember { mutableStateOf(false) }

    SectionTitle(stringResource(R.string.autoscaling_pause_section))
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorOutlinedButton(onClick = { picking = true }, enabled = denial == null && !busy) {
            Text(stringResource(R.string.autoscaling_pause_at))
        }
        if (row.paused) {
            IchorOutlinedButton(onClick = { resuming = true }, enabled = denial == null && !busy) {
                Text(stringResource(R.string.autoscaling_resume))
            }
        }
    }
    KubeDenialNote(denial)

    if (picking) {
        ScaledObjectPauseDialog(
            row,
            onPause = { replicas ->
                picking = false
                if (scaleNeedsTypedName(replicas)) confirmZero = true else onPause(replicas)
            },
            onDismiss = { picking = false },
        )
    }
    if (confirmZero) {
        HostnameConfirmDialog(
            title = stringResource(R.string.autoscaling_pause_zero_title, row.name),
            hostname = row.name,
            confirmLabel = stringResource(R.string.autoscaling_pause_confirm, "0"),
            onConfirm = {
                confirmZero = false
                onPause(0)
            },
            onDismiss = { confirmZero = false },
        ) {
            Text(stringResource(R.string.autoscaling_pause_zero_text, row.target.label.ifEmpty { row.name }), style = MaterialTheme.typography.bodyMedium)
        }
    }
    if (resuming) {
        ConfirmDialog(
            title = stringResource(R.string.autoscaling_resume_title, row.name),
            text = stringResource(R.string.autoscaling_resume_text),
            confirm = stringResource(R.string.autoscaling_resume),
            onConfirm = { resuming = false; onPause(SCALED_OBJECT_RESUME) },
            onDismiss = { resuming = false },
        )
    }
}

/** The count to pause at (the current one first), or hold where it is. */
@Composable
private fun ScaledObjectPauseDialog(row: ScaledObjectRow, onPause: (Int) -> Unit, onDismiss: () -> Unit) {
    var count by rememberSaveable(row.key) { mutableIntStateOf((row.pausedReplicas ?: row.current).coerceIn(0, MAX_SCALE_REPLICAS)) }
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.autoscaling_pause_title, row.name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.autoscaling_pause_text))
                ReplicaStepper(stringResource(R.string.autoscaling_pause_replicas), count, 0..MAX_SCALE_REPLICAS) { count = it }
                IchorTextButton(onClick = { onPause(SCALED_OBJECT_HOLD) }, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.autoscaling_hold))
                }
                Text(
                    stringResource(R.string.autoscaling_hold_text),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { onPause(count) }) { Text(stringResource(R.string.autoscaling_pause_confirm, count.toString())) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/**
 * Edit min/max, disabled with the reason when the credentials may not patch HPAs here; an HPA
 * KEDA owns says where its bounds live instead (KEDA would put them back).
 */
@Composable
fun HpaBoundsSection(row: HpaRow, busy: Boolean, onSave: (Int, Int) -> Unit) {
    SectionTitle(stringResource(R.string.autoscaling_bounds_section))
    if (row.kedaOwned) {
        MutedText(stringResource(R.string.autoscaling_bounds_keda_owned, row.scaledObject))
        return
    }
    val denial = rememberKubeDenial(KubeAction.AUTOSCALE, row.namespace)
    var editing by remember { mutableStateOf(false) }
    IchorOutlinedButton(onClick = { editing = true }, enabled = denial == null && !busy) {
        Text(stringResource(R.string.autoscaling_bounds_edit))
    }
    KubeDenialNote(denial)
    if (editing) {
        HpaBoundsDialog(row, onSave = { min, max -> editing = false; onSave(min, max) }, onDismiss = { editing = false })
    }
}
