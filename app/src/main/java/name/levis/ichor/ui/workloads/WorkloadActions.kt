package name.levis.ichor.ui.workloads

import androidx.compose.runtime.Composable
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
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.argocd.ArgoSelfHealer
import name.levis.ichor.ui.argocd.argoSelfHealer
import name.levis.ichor.ui.argocd.freezeForHandChange
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.uiText

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
    private val kube: KubeRepository,
    private val gitOps: GitOpsRepository,
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
        val outcome = cancellableCatching { kube.scale(workload, replicas) }
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

    /** The Argo CD app that would revert a scale of [workload], from the status already loaded. */
    fun argoOwner(workload: KubeWorkload): ArgoSelfHealer? = gitOps.argoSelfHealer(workload.kind, workload.namespace, workload.name)

    /** Freezes the [healer]'s app for an hour ([reason] recorded with it), then scales [workload] if that worked. */
    fun freezeThenScale(workload: KubeWorkload, replicas: Int, healer: ArgoSelfHealer, reason: String) = run(workload) {
        cancellableCatching { gitOps.freezeForHandChange(healer, reason) }.exceptionOrNull()?.let {
            _messages.send(ActionMessage(UiText.Res(R.string.workloads_scale_freeze_failed, healer.app.name, it.uiText()), error = true))
            return@run
        }
        val outcome = cancellableCatching { kube.scale(workload, replicas) }
        val error = outcome.exceptionOrNull()?.uiText()
        _lastScale.value = ScaleOutcome(workload.key, replicas, outcome.getOrDefault(""), error)
        _messages.send(
            if (error == null) {
                ActionMessage(UiText.Res(R.string.workloads_scale_done_frozen, workload.name, replicas, healer.app.name), error = false)
            } else {
                ActionMessage(UiText.Res(R.string.workloads_scale_failed, workload.name, error), error = true)
            },
        )
        onChanged()
    }

    suspend fun revisions(workload: KubeWorkload): List<KubeRevision> = kube.deploymentRevisions(workload)

    fun rollback(workload: KubeWorkload, revision: KubeRevision) = run(workload) {
        val outcome = cancellableCatching { kube.rollbackDeployment(workload, revision.revision) }
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
fun ActionMessageToasts(messages: Flow<ActionMessage>) = ResultToasts(messages) { context, m -> m.text.resolve(context) to m.error }
