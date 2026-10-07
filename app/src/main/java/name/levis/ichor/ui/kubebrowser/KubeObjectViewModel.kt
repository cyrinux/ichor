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
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.KubeEditPreview
import name.levis.ichor.model.KubeObjectRef
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
 * One object of the browser as YAML ([ref]), never cached (a Secret's values when revealed),
 * and its edit: draft, dry-run diff, then a save at the resourceVersion it was read at, so a
 * concurrent change is refused rather than overwritten.
 */
class KubeObjectViewModel(private val browser: KubeBrowserRepository, val ref: KubeObjectRef) : ViewModel() {
    private val _yaml = MutableStateFlow<UiState<String>>(UiState.Loading)
    val yaml: StateFlow<UiState<String>> = _yaml.asStateFlow()

    private val _revealed = MutableStateFlow(false)
    /** A Secret's values are shown (asked with the app lock's authentication). */
    val revealed: StateFlow<Boolean> = _revealed.asStateFlow()

    private val _edit = MutableStateFlow<ObjectEdit?>(null)
    val edit: StateFlow<ObjectEdit?> = _edit.asStateFlow()

    private val _messages = Channel<UiText>(Channel.BUFFERED)
    /** One-off messages (saved, refused) for a snackbar. */
    val messages: Flow<UiText> = _messages.receiveAsFlow()

    private var load: Job? = null
    private var review: Job? = null

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
                refresh()
            } else {
                _edit.update { it?.copy(saving = false, saveError = outcome.exceptionOrNull()?.uiText()) }
            }
        }
    }
}
