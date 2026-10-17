package name.levis.ichor.ui.node

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubeNodeMeta
import name.levis.ichor.model.NodeMetaEdits
import name.levis.ichor.model.NodeMetaPlan
import name.levis.ichor.model.NodeTaintEdit
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

/**
 * The editor's draft over the node's [original] read: the desired [taints] and [labels], and
 * whether protected ones may change ([showProtected], sent as force).
 */
data class NodeMetaDraft(
    val original: KubeNodeMeta,
    val taints: List<NodeTaintEdit> = original.taints.map { it.edit },
    val labels: Map<String, String> = original.labelMap,
    val showProtected: Boolean = false,
) {
    val edits: NodeMetaEdits get() = NodeMetaEdits.between(original, taints, labels, showProtected)

    /** Whether the draft differs from the read. */
    val changed: Boolean get() = !edits.isEmpty(original)
}

/** The review of a draft: its dry-run plan, with the [edits] it was made for (the apply sends the same). */
sealed interface NodeMetaReview {
    data object Idle : NodeMetaReview
    data object Planning : NodeMetaReview
    data class Ready(val plan: NodeMetaPlan, val edits: NodeMetaEdits, val force: Boolean, val applying: Boolean = false) : NodeMetaReview
    data class Failed(val message: UiText) : NodeMetaReview
}

/** What an apply ended with, for the snackbar: [message] null on success. */
data class NodeMetaOutcome(val message: UiText?)

/** The taints and labels editor of [node] (its Talos address, or its Kubernetes name on a kubeconfig cluster). */
class NodeMetaViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow<UiState<NodeMetaDraft>>(UiState.Loading)
    val state: StateFlow<UiState<NodeMetaDraft>> = _state.asStateFlow()
    private val _review = MutableStateFlow<NodeMetaReview>(NodeMetaReview.Idle)
    val review: StateFlow<NodeMetaReview> = _review.asStateFlow()
    private val _outcome = MutableStateFlow<NodeMetaOutcome?>(null)
    val outcome: StateFlow<NodeMetaOutcome?> = _outcome.asStateFlow()

    init {
        load()
    }

    /** Reads the node again; the draft starts over from it. */
    fun load() {
        val previous = (_state.value as? UiState.Loaded)?.data
        _state.value = previous?.let { UiState.Loaded(it, refreshing = true) } ?: UiState.Loading
        viewModelScope.launch {
            _state.value = uiStateOf { NodeMetaDraft(talos.nodeMeta(node), showProtected = previous?.showProtected ?: false) }
        }
    }

    private fun edit(change: (NodeMetaDraft) -> NodeMetaDraft) {
        _state.update { s -> if (s is UiState.Loaded) s.copy(data = change(s.data)) else s }
    }

    fun showProtected(on: Boolean) = edit { it.copy(showProtected = on) }

    /** Replaces [old] with [new] in the draft's taints, or adds [new] when [old] is null. */
    fun putTaint(old: NodeTaintEdit?, new: NodeTaintEdit) = edit { draft ->
        val kept = draft.taints.filter { it != old && !(it.key == new.key && it.effect == new.effect) }
        draft.copy(taints = kept + new)
    }

    fun removeTaint(taint: NodeTaintEdit) = edit { draft -> draft.copy(taints = draft.taints.filter { it != taint }) }

    /** Sets the label [key] to [value], dropping [oldKey] when the key was renamed. */
    fun putLabel(oldKey: String?, key: String, value: String) = edit { draft ->
        draft.copy(labels = draft.labels.filterKeys { it != oldKey } + (key to value))
    }

    fun removeLabel(key: String) = edit { draft -> draft.copy(labels = draft.labels - key) }

    /** Asks the core for the plan of the draft (a dry run). */
    fun review() {
        val draft = (_state.value as? UiState.Loaded)?.data ?: return
        val edits = draft.edits
        _review.value = NodeMetaReview.Planning
        viewModelScope.launch {
            _review.value = try {
                NodeMetaReview.Ready(talos.nodeMetaPlan(node, edits), edits, draft.showProtected)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                NodeMetaReview.Failed(e.uiText())
            }
        }
    }

    fun dismissReview() {
        if ((_review.value as? NodeMetaReview.Ready)?.applying == true) return
        _review.value = NodeMetaReview.Idle
    }

    /** Applies the reviewed edits; on success the node is read again. */
    fun apply() {
        val ready = _review.value as? NodeMetaReview.Ready ?: return
        if (ready.applying || !ready.plan.canApply) return
        _review.value = ready.copy(applying = true)
        viewModelScope.launch {
            val failure = try {
                talos.nodeMetaApply(node, ready.edits, ready.force)
                null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                e.uiText()
            }
            _review.value = NodeMetaReview.Idle
            _outcome.value = NodeMetaOutcome(failure)
            // A refusal for a stale copy says to reload: the next read is the fresh one either way.
            if (failure == null) load()
        }
    }

    fun outcomeShown() {
        _outcome.value = null
    }
}
