package name.levis.ichor.ui.dataservices

import android.widget.Toast
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.ui.UiText
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
    private val talos: TalosRepository,
    private val onChanged: () -> Unit,
) {
    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    /** Keys (longhornActionKey) of the volumes and nodes with an action in flight. */
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    // A queue, not a state: two actions finishing together each get their message.
    private val _results = Channel<LonghornActionResult>(Channel.BUFFERED)
    val results: Flow<LonghornActionResult> = _results.receiveAsFlow()

    fun run(key: String, namespace: String, name: String, label: String, action: LonghornAction, value: Int = 0) {
        if (key in _busy.value) return
        _busy.update { it + key }
        scope.launch {
            val outcome = runCatching { talos.longhornAction(namespace, name, action, value) }
            _busy.update { it - key }
            _results.send(LonghornActionResult(action, label, value, outcome.exceptionOrNull()?.uiText()))
            if (outcome.isSuccess) onChanged()
        }
    }
}

/** A toast for each outcome of [results]. */
@Composable
fun LonghornResultToasts(results: Flow<LonghornActionResult>) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r ->
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
            Toast.makeText(context, text, if (r.error != null) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
        }
    }
}

/** The ⋮ menu of a volume or node row, a spinner while one of its actions runs. */
@Composable
internal fun LonghornActionMenu(actions: List<LonghornAction>, busy: Boolean, onAction: (LonghornAction) -> Unit) {
    if (busy) {
        Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        }
        return
    }
    var open by remember { mutableStateOf(false) }
    // The Box anchors the menu to the button, not to the row's start edge.
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            actions.forEach { action ->
                DropdownMenuItem(
                    text = { Text(stringResource(action.menuLabel)) },
                    onClick = {
                        open = false
                        onAction(action)
                    },
                )
            }
        }
    }
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
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.longhorn_evict_title, node)) },
        text = { Text(stringResource(R.string.longhorn_evict_text, node)) },
        confirmButton = {
            TextButton(onClick = onConfirm) { Text(stringResource(R.string.longhorn_action_evict), color = LocalStatusColors.current.warn) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
