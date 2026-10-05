package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.CancellationException
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
import name.levis.ichor.data.ARGO_CD
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoProject
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.FREEZE_EXTEND_MINUTES
import name.levis.ichor.model.FreezeScope
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.freezeOptions
import name.levis.ichor.model.projectOf
import name.levis.ichor.model.selfHealingOwner
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** [runCatching] that lets a cancellation through instead of reporting it as a failure. */
internal suspend fun <T> cancellableCatching(block: suspend () -> T): Result<T> = try {
    Result.success(block())
} catch (e: CancellationException) {
    throw e
} catch (e: Throwable) {
    Result.failure(e)
}

/** The last scale of a workload: its [warning] (a HorizontalPodAutoscaler manages it), or why it failed. */
data class ScaleOutcome(val workloadKey: String, val replicas: Int, val warning: String = "", val error: UiText? = null)

/** A message to show once, as a toast. */
data class ActionMessage(val text: UiText, val error: Boolean)

/**
 * Scale and rollback of workloads, in [scope] (a ViewModel's). A rollback opens the rollout
 * status through [restarts], like a restart. [onChanged] runs after a successful change.
 */
class WorkloadActions(
    private val scope: CoroutineScope,
    private val talos: TalosRepository,
    private val restarts: WorkloadRestarts,
    private val onChanged: () -> Unit,
) {
    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the workloads with a scale or rollback in flight. */
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    private val _lastScale = MutableStateFlow<ScaleOutcome?>(null)
    val lastScale: StateFlow<ScaleOutcome?> = _lastScale.asStateFlow()

    private val _messages = Channel<ActionMessage>(Channel.BUFFERED)
    val messages: Flow<ActionMessage> = _messages.receiveAsFlow()

    fun scale(workload: KubeWorkload, replicas: Int) = run(workload) {
        val outcome = cancellableCatching { talos.scale(workload, replicas) }
        val error = outcome.exceptionOrNull()?.uiText()
        _lastScale.value = ScaleOutcome(workload.key, replicas, outcome.getOrDefault(""), error)
        _messages.send(
            if (error == null) {
                ActionMessage(UiText.Res(R.string.workloads_scale_done, workload.name, replicas), error = false)
            } else {
                ActionMessage(UiText.Res(R.string.workloads_scale_failed, workload.name, error), error = true)
            },
        )
        if (error == null) onChanged()
    }

    /**
     * The Argo CD app that would revert a scale of [workload] (self-heal on, not frozen) with
     * its project, from the Argo CD status already loaded; null when none or not loaded.
     */
    fun argoOwner(workload: KubeWorkload): Pair<ArgoApp, ArgoProject>? {
        val status = talos.cached<ArgoStatus>(ARGO_CD)?.value ?: return null
        val app = status.selfHealingOwner(workload.kind, workload.namespace, workload.name) ?: return null
        return status.projectOf(app)?.let { app to it }
    }

    /** Freezes [app] for an hour ([reason] recorded with it), then scales [workload] if that worked. */
    fun freezeThenScale(workload: KubeWorkload, replicas: Int, app: ArgoApp, project: ArgoProject, reason: String) = run(workload) {
        val options = freezeOptions(app, FreezeScope.APP, FREEZE_EXTEND_MINUTES, manualSync = true, reason = reason)
        cancellableCatching { talos.argoFreeze(project.namespace, project.name, ArgoFreezeAction.FREEZE, options) }.exceptionOrNull()?.let {
            _messages.send(ActionMessage(UiText.Res(R.string.workloads_scale_freeze_failed, app.name, it.uiText()), error = true))
            return@run
        }
        val outcome = cancellableCatching { talos.scale(workload, replicas) }
        val error = outcome.exceptionOrNull()?.uiText()
        _lastScale.value = ScaleOutcome(workload.key, replicas, outcome.getOrDefault(""), error)
        _messages.send(
            if (error == null) {
                ActionMessage(UiText.Res(R.string.workloads_scale_done_frozen, workload.name, replicas, app.name), error = false)
            } else {
                ActionMessage(UiText.Res(R.string.workloads_scale_failed, workload.name, error), error = true)
            },
        )
        onChanged()
    }

    suspend fun revisions(workload: KubeWorkload): List<KubeRevision> = talos.deploymentRevisions(workload)

    fun rollback(workload: KubeWorkload, revision: KubeRevision) = run(workload) {
        val outcome = cancellableCatching { talos.rollbackDeployment(workload, revision.revision) }
        outcome.exceptionOrNull()?.let {
            _messages.send(ActionMessage(UiText.Res(R.string.workloads_rollback_failed, workload.name, it.uiText()), error = true))
            return@run
        }
        restarts.follow(workload)
        onChanged()
    }

    private fun run(workload: KubeWorkload, block: suspend () -> Unit) {
        if (workload.key in _busy.value) return
        _busy.update { it + workload.key }
        scope.launch {
            try {
                block()
            } finally {
                _busy.update { it - workload.key }
            }
        }
    }
}

/** A toast for each message of [messages]. */
@Composable
fun ActionMessageToasts(messages: Flow<ActionMessage>) {
    val context = LocalContext.current
    LaunchedEffect(messages) {
        messages.collect { m -> Toast.makeText(context, m.text.resolve(context), if (m.error) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show() }
    }
}
