package name.levis.ichor.ui.updates

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.UpdatesRepository
import name.levis.ichor.model.ClusterUpdates
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.model.UpdateEditApp
import name.levis.ichor.model.UpdateRow
import name.levis.ichor.model.updateQuickEdit
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.refreshFailed
import name.levis.ichor.ui.uiText

/** Where a tapped candidate goes: the file at its line with the image sheet prefilled, or the editor on a field's edit. */
sealed interface UpdateOpen {
    val app: UpdateEditApp
    val sourceIndex: Int
    val path: String

    /** The file at [line], with [prefill] (an image edit) opening its sheet; null for a values file to edit by hand. */
    data class File(override val app: UpdateEditApp, override val sourceIndex: Int, override val path: String, val line: Int, val prefill: QuickEdit?) : UpdateOpen

    /** The editor with [edit] applied, on its review. */
    data class Edit(override val app: UpdateEditApp, override val sourceIndex: Int, override val path: String, val edit: QuickEdit) : UpdateOpen
}

/**
 * The Updates screen: [needsConsent] until the one-time registry notice is accepted for the
 * cluster, then the lookup (kept for the session: a visit shows the last answer, a pull asks
 * again). [resolving] names the row whose file is being found; [notes] say why a row has no edit.
 */
data class UpdatesUiState(
    val updates: UiState<ClusterUpdates> = UiState.Loading,
    val needsConsent: Boolean = false,
    /** What the last answer looked up: the progress line's count while asking again. */
    val lastQueried: Int = 0,
    val resolving: String? = null,
    val notes: Map<String, UiText> = emptyMap(),
    /** A rendered image's values file opened: the screen says where the tag is. */
    val renderedHint: Boolean = false,
)

class UpdatesViewModel(private val repo: UpdatesRepository) : ViewModel() {
    private val _state = MutableStateFlow(UpdatesUiState())
    val state: StateFlow<UpdatesUiState> = _state.asStateFlow()
    private val _open = MutableStateFlow<UpdateOpen?>(null)

    /** A file or editor to navigate to; the screen calls [consumeOpen] once it has. */
    val open: StateFlow<UpdateOpen?> = _open.asStateFlow()
    private var source: Any? = null
    private var job: Job? = null

    /** Loads once per [key] (the cluster, the config generation): the session's last answer, else a lookup once consented. */
    fun load(key: Any) {
        if (key == source) return
        source = key
        job?.cancel()
        val last = repo.lastResult()
        _state.value = UpdatesUiState(
            updates = last?.let { UiState.Loaded(it, fetchedAt = it.at) } ?: UiState.Loading,
            needsConsent = last == null && !repo.consented(),
            lastQueried = last?.lookups?.queried ?: 0,
        )
        if (last == null && !_state.value.needsConsent) fetch()
    }

    fun consent() {
        repo.consent()
        _state.update { it.copy(needsConsent = false) }
        fetch()
    }

    fun refresh() {
        if (_state.value.needsConsent) return
        fetch()
    }

    private fun fetch() {
        job?.cancel()
        _state.update { s ->
            val u = s.updates
            s.copy(updates = if (u is UiState.Loaded) u.copy(refreshing = true) else UiState.Loading)
        }
        job = viewModelScope.launch {
            cancellableCatching { repo.clusterUpdates() }.fold(
                onSuccess = { r -> _state.update { it.copy(updates = UiState.Loaded(r, fetchedAt = r.at), lastQueried = r.lookups.queried) } },
                onFailure = { e -> _state.update { it.copy(updates = it.updates.refreshFailed(e.uiText())) } },
            )
        }
    }

    /** The candidate [version] of [row] tapped: finds its file, then [open] says where to go (or a note on the row why not). */
    fun pick(row: UpdateRow, version: String) {
        if (_state.value.resolving != null || !row.editable) return
        _state.update { it.copy(resolving = row.key, notes = it.notes - row.key) }
        viewModelScope.launch {
            val outcome = cancellableCatching { repo.editTarget(row, version) }
            val target = outcome.getOrElse { e ->
                _state.update { it.copy(resolving = null, notes = it.notes + (row.key to e.uiText())) }
                return@launch
            }
            val app = target.app
            val edit = updateQuickEdit(target, row, version)
            val next = when {
                app == null || target.path.isEmpty() -> null
                edit == null -> UpdateOpen.File(app, target.sourceIndex, target.path, target.line, null)
                edit.op == QuickEdit.OP_PATH -> UpdateOpen.Edit(app, target.sourceIndex, target.path, edit)
                else -> UpdateOpen.File(app, target.sourceIndex, target.path, target.line, edit)
            }
            _state.update {
                it.copy(
                    resolving = null,
                    notes = if (next == null) it.notes + (row.key to UiText.Raw(target.note)) else it.notes,
                    renderedHint = next is UpdateOpen.File && next.prefill == null,
                )
            }
            _open.value = next
        }
    }

    fun consumeOpen() {
        _open.value = null
    }

    fun consumeRenderedHint() {
        _state.update { it.copy(renderedHint = false) }
    }
}
