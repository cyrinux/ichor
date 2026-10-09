package name.levis.ichor.ui.kubebrowser

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.KUBE_WATCH_RETRY_MILLIS
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.watchForever
import name.levis.ichor.model.DeletePropagation
import name.levis.ichor.model.KubeDeletePreview
import name.levis.ichor.model.KubeEditPreview
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubeObjectScale
import name.levis.ichor.model.KubeObjectSummary
import name.levis.ichor.model.clampReplicas
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.uiText

/**
 * An edit of the object's YAML: the [draft] typed from [original]; [review] once the user
 * asked what saving would change (a dry run), with [saving] and [saveError] for the save.
 */
data class ObjectEdit(
    val original: String,
    val draft: String,
    val review: UiState<KubeEditPreview>? = null,
    val saving: Boolean = false,
    val saveError: UiText? = null,
) {
    val changed: Boolean get() = draft != original
}

/**
 * A deletion being confirmed: what it would do ([preview]), the [propagation] chosen, then
 * [deleting] while it runs and [error] when it was refused.
 */
data class ObjectDelete(
    val preview: UiState<KubeDeletePreview> = UiState.Loading,
    val propagation: DeletePropagation = DeletePropagation.BACKGROUND,
    val deleting: Boolean = false,
    val error: UiText? = null,
)

/**
 * A scale being chosen: the object's count ([scale], read when the dialog opens), the
 * [target] picked, then [applying] while it runs and [error] when it was refused.
 */
data class ObjectScale(
    val scale: UiState<KubeObjectScale> = UiState.Loading,
    val target: Int = 0,
    val applying: Boolean = false,
    val error: UiText? = null,
)

/**
 * One object of the browser ([ref]): its summary (conditions, owners, events), its YAML,
 * never cached (a Secret's values when revealed), and its edit: draft, dry-run diff, then a
 * save at the resourceVersion it was read at, so a concurrent change is refused rather than
 * overwritten.
 */
class KubeObjectViewModel(private val browser: KubeBrowserRepository, val ref: KubeObjectRef) : ViewModel() {
    private val _yaml = MutableStateFlow<UiState<String>>(UiState.Loading)
    val yaml: StateFlow<UiState<String>> = _yaml.asStateFlow()

    private val _summary = MutableStateFlow<UiState<KubeObjectSummary>>(UiState.Loading)
    val summary: StateFlow<UiState<KubeObjectSummary>> = _summary.asStateFlow()

    private val _revealed = MutableStateFlow(false)
    /** A Secret's values are shown (asked with the app lock's authentication). */
    val revealed: StateFlow<Boolean> = _revealed.asStateFlow()

    private val _edit = MutableStateFlow<ObjectEdit?>(null)
    val edit: StateFlow<ObjectEdit?> = _edit.asStateFlow()

    private val _messages = Channel<UiText>(Channel.BUFFERED)
    /** One-off messages (saved, refused) for a snackbar. */
    val messages: Flow<UiText> = _messages.receiveAsFlow()

    private val _delete = MutableStateFlow<ObjectDelete?>(null)
    /** The deletion being confirmed, null when none is. */
    val delete: StateFlow<ObjectDelete?> = _delete.asStateFlow()

    private val _deleted = Channel<Unit>(Channel.CONFLATED)
    /** Once the object is deleted: the screen leaves. */
    val deleted: Flow<Unit> = _deleted.receiveAsFlow()

    private val _scale = MutableStateFlow<ObjectScale?>(null)
    /** The scale being chosen, null when none is. */
    val scale: StateFlow<ObjectScale?> = _scale.asStateFlow()

    private var load: Job? = null
    private var scaleLoad: Job? = null
    private var deletePreview: Job? = null
    private var summaryLoad: Job? = null
    private var review: Job? = null

    /** Reads the summary and the YAML again. */
    fun refreshAll() {
        refreshSummary()
        refresh()
    }

    fun refreshSummary() {
        summaryLoad?.cancel()
        val previous = _summary.value
        _summary.value = if (previous is UiState.Loaded) previous.copy(refreshing = true) else UiState.Loading
        summaryLoad = viewModelScope.launch {
            _summary.value = cancellableCatching { browser.objectSummary(ref) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
        }
    }

    /**
     * Keeps the summary live while called (the screen is visible): the Go core reads it again
     * whenever the object or its events change. A watch that ends is followed again after
     * [KUBE_WATCH_RETRY_MILLIS]; one refused leaves the one-shot read on screen.
     */
    suspend fun followSummary() {
        watchForever(start = { browser.objectSummaryWatch(ref) }) { item ->
            when (item) {
                is StreamItem.Item -> _summary.value = UiState.Loaded(item.value)
                is StreamItem.Done -> if (_summary.value is UiState.Loading) item.error?.let { _summary.value = UiState.Failed(UiText.Raw(it)) }
            }
        }
    }

    fun refresh() {
        if (_edit.value != null) return
        load?.cancel()
        val previous = _yaml.value
        _yaml.value = if (previous is UiState.Loaded) previous.copy(refreshing = true) else UiState.Loading
        val reveal = _revealed.value
        load = viewModelScope.launch {
            _yaml.value = cancellableCatching { browser.objectYaml(ref, reveal) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
        }
    }

    /** Shows or hides a Secret's values, reading it again; not while editing. */
    fun reveal(on: Boolean) {
        if (_revealed.value == on || _edit.value != null) return
        _revealed.value = on
        // The revealed text must not stay on screen while the hidden one loads.
        _yaml.value = UiState.Loading
        refresh()
    }

    /** Starts editing the YAML on screen; a Secret's values must be shown first (hidden ones would be saved as such). */
    fun startEdit() {
        val text = (_yaml.value as? UiState.Loaded)?.data ?: return
        if (!ref.editable) return
        if (ref.isSecret && !_revealed.value) {
            _messages.trySend(UiText.Res(R.string.kb_edit_reveal_first))
            return
        }
        _edit.value = ObjectEdit(original = text, draft = text)
    }

    fun changeDraft(text: String) = _edit.update { it?.copy(draft = text, saveError = null) }

    /** Leaves the editor, dropping the draft. */
    fun cancelEdit() {
        review?.cancel()
        _edit.value = null
    }

    /** Asks the API server what saving the draft would store (a dry run) and shows the diff. */
    fun review() {
        val edit = _edit.value ?: return
        review?.cancel()
        _edit.value = edit.copy(review = UiState.Loading, saveError = null)
        review = viewModelScope.launch {
            val result = cancellableCatching { browser.updatePreview(ref, edit.draft) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
            _edit.update { it?.copy(review = result) }
        }
    }

    /** Back from the diff to the draft. */
    fun backToEditor() {
        review?.cancel()
        _edit.update { it?.copy(review = null, saving = false) }
    }

    /** Saves the draft; on success leaves the editor and reads the object again. */
    fun save() {
        val edit = _edit.value ?: return
        if (edit.saving) return
        _edit.value = edit.copy(saving = true, saveError = null)
        viewModelScope.launch {
            val outcome = cancellableCatching { browser.update(ref, edit.draft) }
            if (outcome.isSuccess) {
                _edit.value = null
                _messages.send(UiText.Res(R.string.kb_saved))
                refreshAll()
            } else {
                _edit.update { it?.copy(saving = false, saveError = outcome.exceptionOrNull()?.uiText()) }
            }
        }
    }

    /** Opens the deletion's confirmation and reads what it would do. */
    fun startDelete() {
        if (_delete.value != null) return
        _delete.value = ObjectDelete()
        loadDeletePreview()
    }

    fun loadDeletePreview() {
        deletePreview?.cancel()
        _delete.update { it?.copy(preview = UiState.Loading, error = null) }
        deletePreview = viewModelScope.launch {
            val result = cancellableCatching { browser.deletePreview(ref) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
            _delete.update { it?.copy(preview = result) }
        }
    }

    fun choosePropagation(propagation: DeletePropagation) = _delete.update { it?.copy(propagation = propagation, error = null) }

    fun cancelDelete() {
        deletePreview?.cancel()
        _delete.value = null
    }

    /**
     * Deletes the object at the version the preview read; [force] for a protected one (its
     * name typed). On success the screen leaves ([deleted]).
     */
    fun confirmDelete(force: Boolean) {
        val current = _delete.value ?: return
        val preview = (current.preview as? UiState.Loaded)?.data ?: return
        if (current.deleting) return
        _delete.value = current.copy(deleting = true, error = null)
        viewModelScope.launch {
            val outcome = cancellableCatching { browser.delete(ref, current.propagation, preview.resourceVersion, force) }
            if (outcome.isSuccess) {
                _delete.value = null
                _deleted.send(Unit)
            } else {
                _delete.update { it?.copy(deleting = false, error = outcome.exceptionOrNull()?.uiText()) }
            }
        }
    }

    /** Opens the scale dialog and reads the object's count. */
    fun startScale() {
        if (_scale.value != null || !ref.scalable) return
        _scale.value = ObjectScale()
        loadScale()
    }

    fun loadScale() {
        scaleLoad?.cancel()
        _scale.update { it?.copy(scale = UiState.Loading, error = null) }
        scaleLoad = viewModelScope.launch {
            val result = cancellableCatching { browser.objectScale(ref) }
            _scale.update { current ->
                current?.copy(
                    scale = result.fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) }),
                    target = result.getOrNull()?.replicas ?: current.target,
                )
            }
        }
    }

    fun chooseScale(target: Int) = _scale.update { it?.copy(target = clampReplicas(target), error = null) }

    fun cancelScale() {
        scaleLoad?.cancel()
        _scale.value = null
    }

    /** Applies the target; on success closes the dialog, says so (and which autoscaler will undo it), and reads the object again. */
    fun applyScale() {
        val current = _scale.value ?: return
        if (current.applying || current.scale !is UiState.Loaded) return
        val target = current.target
        _scale.value = current.copy(applying = true, error = null)
        viewModelScope.launch {
            val outcome = cancellableCatching { browser.scale(ref, target) }
            outcome.onSuccess { warning ->
                _scale.value = null
                _messages.send(UiText.Res(R.string.workloads_scale_done, ref.name, target))
                if (warning.isNotEmpty()) _messages.send(UiText.Raw(warning))
                refreshAll()
            }.onFailure { e ->
                _scale.update { it?.copy(applying = false, error = e.uiText()) }
            }
        }
    }
}
