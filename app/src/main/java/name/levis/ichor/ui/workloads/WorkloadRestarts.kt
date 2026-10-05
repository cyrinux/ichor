package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
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
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** Outcome of a rollout restart, shown once. */
data class RestartResult(val workload: KubeWorkload, val error: UiText?)

/**
 * Rollout restarts run in [scope] (a ViewModel's): which are in flight, how each ended, and
 * the one whose rollout is followed live after it started (see [RolloutStatusSheet]).
 * [onRestarted] runs after a successful one, e.g. to show the rollout starting, and again
 * when the followed rollout ends.
 */
class WorkloadRestarts(
    private val scope: CoroutineScope,
    private val talos: TalosRepository,
    private val onRestarted: () -> Unit,
) {
    private val _restarting = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the workloads whose restart request is in flight. */
    val restarting: StateFlow<Set<String>> = _restarting.asStateFlow()

    // A queue, not a state: two restarts finishing together each get their message.
    private val _results = Channel<RestartResult>(Channel.BUFFERED)
    val results: Flow<RestartResult> = _results.receiveAsFlow()

    private val _following = MutableStateFlow<KubeWorkload?>(null)
    /** The workload whose rollout is shown live; null once the user stops following it. */
    val following: StateFlow<KubeWorkload?> = _following.asStateFlow()

    /** [workload] as it is now (replicas, state), for its confirmation; [workload] if it cannot be read. */
    suspend fun current(workload: KubeWorkload): KubeWorkload =
        cancellableCatching { talos.workload(workload.kind, workload.namespace, workload.name) }.getOrDefault(workload)

    fun restart(workload: KubeWorkload) {
        if (workload.key in _restarting.value) return
        _restarting.update { it + workload.key }
        scope.launch {
            val outcome = runCatching { talos.rolloutRestart(workload) }
            _restarting.update { it - workload.key }
            _results.send(RestartResult(workload, outcome.exceptionOrNull()?.uiText()))
            if (outcome.isSuccess) {
                _following.value = workload
                onRestarted()
            }
        }
    }

    suspend fun rolloutStatus(workload: KubeWorkload): KubeRolloutStatus = talos.rolloutStatus(workload)

    /** Follows [workload]'s rollout live, e.g. after a rollback. */
    fun follow(workload: KubeWorkload) {
        _following.value = workload
    }

    /** Stops following the rollout: it goes on in the cluster. */
    fun stopFollowing() {
        _following.value = null
    }

    /** The followed rollout ended: show its final state. */
    fun rolloutEnded() = onRestarted()
}

/** A toast for each failed restart of [results]; a successful one opens [RolloutStatusSheet]. */
@Composable
fun RestartResultToasts(results: Flow<RestartResult>) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r ->
            val error = r.error?.resolve(context) ?: return@collect
            Toast.makeText(context, context.getString(R.string.workloads_restart_failed, r.workload.name, error), Toast.LENGTH_LONG).show()
        }
    }
}

@Composable
fun RestartConfirmDialog(workload: KubeWorkload, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.workloads_restart_title, workload.kind, workload.name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.workloads_restart_text, workload.namespace))
                if (workload.desired <= 1) {
                    Text(stringResource(R.string.workloads_restart_single), color = LocalStatusColors.current.warn)
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) { Text(stringResource(R.string.workloads_restart_confirm), color = LocalStatusColors.current.bad) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
