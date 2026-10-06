package name.levis.ichor.ui.machineconfig

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.ConfigPreview
import name.levis.ichor.model.ConfigTree
import name.levis.ichor.model.ConfigTryCommand
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.model.after
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

/**
 * What the machine config screen shows besides the node's YAML: its tree, and the draft
 * being edited with where it stands (reviewed, tried).
 */
data class ConfigEditorState(
    /** The tree of the draft while editing, of the node's config otherwise. */
    val tree: ConfigTree? = null,
    /** The redacted config the draft was made from. */
    val base: String = "",
    /** Non-null while editing. */
    val draft: String? = null,
    /** An edit is being applied to the draft. */
    val busy: Boolean = false,
    /** Non-null while the change is reviewed. */
    val review: UiState<ConfigPreview>? = null,
    /** Non-null once the change was sent to the node. */
    val run: ConfigTryState? = null,
) {
    val editing: Boolean get() = draft != null
    val dirty: Boolean get() = draft != null && draft != base
    val canReview: Boolean get() = dirty && !busy && tree?.error == null
}

/** How long typing in the YAML editor rests before the draft is parsed again. */
private const val DESCRIBE_DEBOUNCE_MS = 400L

/**
 * The node's machine config (os:admin): redacted unless the user asked to reveal secrets,
 * and editable as a draft that is tried on the node with an automatic revert.
 */
class MachineConfigViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<String>() {
    private val _revealed = MutableStateFlow(false)
    val revealed: StateFlow<Boolean> = _revealed.asStateFlow()

    private val _editor = MutableStateFlow(ConfigEditorState())
    val editor: StateFlow<ConfigEditorState> = _editor.asStateFlow()

    private val _messages = MutableSharedFlow<UiText>(extraBufferCapacity = 4)

    /** Refused edits, to show once. */
    val messages: SharedFlow<UiText> = _messages.asSharedFlow()

    private val commands = MutableSharedFlow<ConfigTryCommand>(extraBufferCapacity = 1)

    /** The Talos version whose schema describes the tree; empty until known (or when it cannot be). */
    private var schemaVersion = ""
    private var editWhenLoaded = false
    private var describing: Job? = null
    private var trying: Job? = null

    init {
        // The schema is downloaded once per Talos version; the tree shows without it meanwhile.
        viewModelScope.launch {
            val status = runCatching { talos.machineConfigSchema(node) }.getOrNull() ?: return@launch
            schemaVersion = status.version
            if (status.available) currentText()?.let { describe(it) }
        }
    }

    // Not cached anywhere: the revealed config holds the cluster's secrets.
    override suspend fun fetch(): String {
        val yaml = talos.machineConfig(node, _revealed.value)
        val edit = editWhenLoaded && !_revealed.value
        editWhenLoaded = false
        _editor.update { ConfigEditorState(base = if (edit) yaml else "", draft = if (edit) yaml else null) }
        describe(yaml)
        return yaml
    }

    fun reveal(on: Boolean) {
        if (_revealed.value == on) return
        _revealed.value = on
        refresh(reset = true) // never show secrets after hiding them, even while refetching
    }

    /** Starts a draft from the redacted config: a draft must never hold the real secrets. */
    fun startEditing() {
        val loaded = (state.value as? UiState.Loaded)?.data ?: return
        if (_revealed.value) {
            editWhenLoaded = true
            reveal(false)
            return
        }
        _editor.update { it.copy(base = loaded, draft = loaded, review = null, run = null) }
    }

    /** Drops the draft; the node was not changed. */
    fun discard() {
        describing?.cancel()
        _editor.update { ConfigEditorState(tree = it.tree) }
        (state.value as? UiState.Loaded)?.data?.let { text -> viewModelScope.launch { describe(text) } }
    }

    /** The YAML editor's text: kept at once, parsed when typing rests. */
    fun setDraft(text: String) {
        if (_editor.value.draft == null) return
        _editor.update { it.copy(draft = text) }
        describing?.cancel()
        describing = viewModelScope.launch {
            delay(DESCRIBE_DEBOUNCE_MS)
            describe(text)
        }
    }

    /** One field edit from the tree. A refused edit leaves the draft as it was and says why. */
    fun apply(edit: ConfigEdit) {
        val draft = _editor.value.draft ?: return
        if (_editor.value.busy) return
        _editor.update { it.copy(busy = true) }
        viewModelScope.launch {
            try {
                val edited = talos.machineConfigEdit(draft, edit)
                _editor.update { it.copy(draft = edited) }
                describe(edited)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _messages.tryEmit(e.uiText())
            } finally {
                _editor.update { it.copy(busy = false) }
            }
        }
    }

    /** Asks the node what the draft would change. */
    fun review() {
        val editor = _editor.value
        val draft = editor.draft ?: return
        _editor.update { it.copy(review = UiState.Loading) }
        viewModelScope.launch {
            val preview = uiStateOf { talos.machineConfigPreview(node, editor.base, draft) }
            _editor.update { if (it.review != null) it.copy(review = preview) else it }
        }
    }

    fun closeReview() = _editor.update { it.copy(review = null) }

    /** Applies the draft in try mode: the node reverts after [timeoutSeconds] unless [keep] is called. */
    fun startTry(timeoutSeconds: Int) {
        val editor = _editor.value
        val draft = editor.draft ?: return
        if (trying?.isActive == true) return
        _editor.update { it.copy(run = ConfigTryState.Running(ConfigTryState.APPLYING)) }
        trying = viewModelScope.launch {
            talos.tryMachineConfig(node, editor.base, draft, timeoutSeconds, commands).collect { event ->
                _editor.update { it.copy(run = it.run.after(event)) }
            }
        }
    }

    fun keep() {
        commands.tryEmit(ConfigTryCommand.KEEP)
    }

    fun revertNow() {
        commands.tryEmit(ConfigTryCommand.REVERT)
    }

    /** Leaves a finished try: back to the node's config, read again. */
    fun finishTry() {
        if (_editor.value.run is ConfigTryState.Running) return
        trying?.cancel()
        _editor.update { ConfigEditorState(tree = it.tree) }
        refresh(reset = true)
    }

    private fun currentText(): String? = _editor.value.draft ?: (state.value as? UiState.Loaded)?.data

    private suspend fun describe(text: String) {
        val tree = runCatching { talos.machineConfigDescribe(text, schemaVersion) }.getOrNull() ?: return
        // A newer text may have come meanwhile: only the tree of what is shown is kept.
        _editor.update { if (it.draft == null || it.draft == text) it.copy(tree = tree) else it }
    }
}
