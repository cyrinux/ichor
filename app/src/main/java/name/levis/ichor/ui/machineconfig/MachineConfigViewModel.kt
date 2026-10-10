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
import name.levis.ichor.R
import name.levis.ichor.data.ConfigTryManager
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.MultiApplyRun
import name.levis.ichor.model.MultiConfigPreview
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigApplyState
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.ConfigPreview
import name.levis.ichor.model.ConfigSyntaxError
import name.levis.ichor.model.ConfigTree
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.model.after
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.userMessage

/**
 * What the machine config screen shows besides the node's YAML: its tree, and the draft
 * being edited with where it stands (reviewed, tried).
 */
data class ConfigEditorState(
    /** The tree of the draft while editing, of the node's config otherwise. */
    val tree: ConfigTree? = null,
    /** The draft changed since [tree] was made: its paths may point at other fields. */
    val treeStale: Boolean = false,
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
    /** A change applied for good (not tried): its progress, then how it ended. */
    val apply: ConfigApplyState? = null,
    /** The field edits made to the draft, in order: what other nodes can be given. */
    val edits: List<ConfigEdit> = emptyList(),
    /** False once the YAML was typed into: that change is text, it cannot be replayed elsewhere. */
    val replayable: Boolean = true,
    /** Non-null while the change is taken to other nodes too. */
    val multi: MultiConfigState? = null,
) {
    val editing: Boolean get() = draft != null
    val dirty: Boolean get() = draft != null && draft != base
    val canReview: Boolean get() = dirty && !busy && !treeStale && tree?.error == null

    /** The draft is only field edits, which other nodes' configs can be given too. */
    val canReplay: Boolean get() = replayable && edits.isNotEmpty()
}

/**
 * The same change on several nodes: picking them ([picking]), each node's preview, then the
 * run. [cluster] is what is typed to confirm.
 */
data class MultiConfigState(
    val candidates: UiState<List<NodeOverview>> = UiState.Loading,
    val cluster: String = "",
    val selected: Set<String> = emptySet(),
    val picking: Boolean = true,
    val preview: UiState<MultiConfigPreview>? = null,
    val run: MultiApplyRun? = null,
)

/** How long typing in the YAML editor rests before the draft is parsed again. */
private const val DESCRIBE_DEBOUNCE_MS = 400L

/**
 * The node's machine config (os:admin): redacted unless the user asked to reveal secrets,
 * and editable as a draft that is tried on the node with an automatic revert.
 */
class MachineConfigViewModel(
    private val talos: TalosRepository,
    private val node: String,
    private val hostname: String,
    /** Runs the try app-wide: its countdown and Keep outlive this screen. */
    private val tries: ConfigTryManager,
) : LoadingViewModel<String>() {
    private val _revealed = MutableStateFlow(false)
    val revealed: StateFlow<Boolean> = _revealed.asStateFlow()

    private val _editor = MutableStateFlow(ConfigEditorState())
    val editor: StateFlow<ConfigEditorState> = _editor.asStateFlow()

    private val _messages = MutableSharedFlow<UiText>(extraBufferCapacity = 4)

    /** Refused edits, to show once. */
    val messages: SharedFlow<UiText> = _messages.asSharedFlow()

    /** The Talos version whose schema describes the tree; empty until known (or when it cannot be). */
    private var schemaVersion = ""
    private var editWhenLoaded = false
    private var describing: Job? = null
    private var applying: Job? = null
    private var reviewing: Job? = null
    private var trying: Job? = null
    private var multiJob: Job? = null

    init {
        // A try of this node is shown from the app-wide run, also when the screen is reopened.
        viewModelScope.launch {
            tries.current.collect { run ->
                if (run?.node == node) _editor.update { it.copy(run = run.state) }
            }
        }
        // The schema is downloaded once per Talos version; the tree shows without it meanwhile.
        viewModelScope.launch {
            val status = runCatching { talos.machineConfigSchema(node) }.getOrNull() ?: return@launch
            schemaVersion = status.version
            if (status.available) currentText()?.let { describe(it) }
        }
    }

    // Not cached anywhere: the revealed config holds the cluster's secrets.
    override suspend fun fetch(): String {
        val edit = editWhenLoaded
        editWhenLoaded = false
        val yaml = talos.machineConfig(node, _revealed.value)
        // A draft only ever starts from the redacted config.
        val draft = yaml.takeIf { edit && !_revealed.value }
        _editor.update { ConfigEditorState(base = draft.orEmpty(), draft = draft) }
        describe(yaml)
        return yaml
    }

    fun reveal(on: Boolean) {
        if (_revealed.value == on || _editor.value.editing) return
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
        _editor.update { it.copy(base = loaded, draft = loaded, review = null, run = null, edits = emptyList(), replayable = true) }
    }

    /** Drops the draft; the node was not changed. */
    fun discard() {
        describing?.cancel()
        applying?.cancel()
        reviewing?.cancel()
        _editor.update { ConfigEditorState(tree = it.tree, treeStale = true) }
        (state.value as? UiState.Loaded)?.data?.let { text -> viewModelScope.launch { describe(text) } }
    }

    /** The YAML editor's text: kept at once, parsed when typing rests. */
    fun setDraft(text: String) {
        val current = _editor.value.draft ?: return
        if (current == text) return
        _editor.update { it.copy(draft = text, treeStale = true, replayable = false) }
        describing?.cancel()
        describing = viewModelScope.launch {
            delay(DESCRIBE_DEBOUNCE_MS)
            describe(text)
        }
    }

    /** One field edit from the tree. A refused edit leaves the draft as it was and says why. */
    fun apply(edit: ConfigEdit) {
        val editor = _editor.value
        val draft = editor.draft ?: return
        // The edit's path comes from the tree: it must be the tree of this very draft.
        if (editor.busy || editor.treeStale) return
        _editor.update { it.copy(busy = true) }
        applying = viewModelScope.launch {
            try {
                val edited = talos.machineConfigEdit(draft, edit)
                // Only when the draft is still the one that was edited (not discarded meanwhile).
                if (_editor.value.draft == draft) {
                    _editor.update { it.copy(draft = edited, treeStale = true, edits = it.edits + edit) }
                    describe(edited)
                }
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
        reviewing?.cancel()
        _editor.update { it.copy(review = UiState.Loading) }
        reviewing = viewModelScope.launch {
            val preview = uiStateOf { talos.machineConfigPreview(node, editor.base, draft) }
            // The diff shown must be the one of the draft that a try would send.
            _editor.update { if (it.review != null && it.draft == draft) it.copy(review = preview) else it }
        }
    }

    fun closeReview() {
        reviewing?.cancel()
        _editor.update { it.copy(review = null) }
    }

    /**
     * Applies the draft in try mode: the node reverts after [timeoutSeconds] unless [keep] is
     * called. Refused while another try runs (one at a time, across nodes).
     */
    fun startTry(timeoutSeconds: Int) {
        val editor = _editor.value
        val draft = editor.draft ?: return
        if (trying?.isActive == true || editor.review !is UiState.Loaded) return
        tries.current.value?.takeIf { it.running }?.let { other ->
            _messages.tryEmit(UiText.Res(R.string.config_try_already_running, other.hostname))
            return
        }
        tries.dismiss() // a finished try of another node is not shown any more
        tries.start(node, hostname, editor.base, draft, timeoutSeconds)
    }

    /** Applies the draft for good in [mode] (StartConfigApply). */
    fun startApply(mode: ConfigApplyMode) {
        val editor = _editor.value
        val draft = editor.draft ?: return
        if (trying?.isActive == true || editor.review !is UiState.Loaded) return
        _editor.update { it.copy(apply = ConfigApplyState.Running(mode)) }
        trying = viewModelScope.launch {
            try {
                talos.applyMachineConfig(node, editor.base, draft, mode).collect { event ->
                    _editor.update { state -> state.copy(apply = state.apply?.after(event)) }
                }
                // The run always ends with how it ended; without it, nothing can be said.
                _editor.update { if (it.apply is ConfigApplyState.Running) it.copy(apply = ConfigApplyState.Failed(mode, "")) else it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _editor.update { it.copy(apply = ConfigApplyState.Failed(mode, e.userMessage())) }
            }
        }
    }

    /**
     * Leaves a finished apply. Done: back to the node's config, read again. Failed: back to the
     * draft, still there to fix or apply again.
     */
    fun finishApply() {
        when (_editor.value.apply) {
            null, is ConfigApplyState.Running -> return
            is ConfigApplyState.Failed -> _editor.update { it.copy(apply = null, review = null) }
            is ConfigApplyState.Done -> {
                _editor.update { ConfigEditorState(tree = it.tree, treeStale = true) }
                refresh(reset = true)
            }
        }
    }

    /** Starts taking the change to other nodes: this node picked, the others offered. */
    fun startMulti() {
        if (!_editor.value.canReplay) return
        _editor.update { it.copy(multi = MultiConfigState(selected = setOf(node))) }
        multiJob?.cancel()
        multiJob = viewModelScope.launch {
            val overview = uiStateOf { talos.cached<ClusterOverview>(OVERVIEW)?.value ?: talos.overview() }
            _editor.update { state ->
                val multi = state.multi ?: return@update state
                val candidates = when (overview) {
                    is UiState.Loaded -> UiState.Loaded(overview.data.nodes.filter { it.reachable || it.node == node })
                    is UiState.Failed -> overview
                    UiState.Loading -> UiState.Loading
                }
                state.copy(multi = multi.copy(candidates = candidates, cluster = (overview as? UiState.Loaded)?.data?.context.orEmpty()))
            }
        }
    }

    fun toggleMultiNode(address: String) = updateMulti { m ->
        m.copy(selected = if (address in m.selected) m.selected - address else m.selected + address)
    }

    /** Each picked node's own diff. */
    fun previewMulti() {
        val editor = _editor.value
        val multi = editor.multi ?: return
        val nodes = (multi.candidates as? UiState.Loaded)?.data?.map { it.node }?.filter { it in multi.selected } ?: return
        if (nodes.isEmpty()) return
        updateMulti { it.copy(picking = false, preview = UiState.Loading) }
        multiJob?.cancel()
        multiJob = viewModelScope.launch {
            val preview = uiStateOf { talos.machineConfigMultiPreview(nodes, editor.edits) }
            updateMulti { if (it.preview != null) it.copy(preview = preview) else it }
        }
    }

    /** Applies the edits to the nodes the preview would change, one after the other. */
    fun startMultiApply(mode: ConfigApplyMode) {
        val editor = _editor.value
        val preview = (editor.multi?.preview as? UiState.Loaded)?.data ?: return
        val nodes = preview.changing.map { it.node }
        if (nodes.isEmpty() || trying?.isActive == true) return
        updateMulti { it.copy(run = MultiApplyRun(mode)) }
        trying = viewModelScope.launch {
            try {
                talos.applyMachineConfigMulti(nodes, editor.edits, mode).collect { event ->
                    updateMulti { m -> m.copy(run = m.run?.after(event)) }
                }
                updateMulti { m -> m.run?.takeIf { !it.finished }?.let { m.copy(run = it.copy(finished = true, error = "")) } ?: m }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                updateMulti { m -> m.copy(run = m.run?.copy(finished = true, error = e.userMessage())) }
            }
        }
    }

    /** Back from picking or from the per-node review: to this node's review. */
    fun closeMulti() {
        if (_editor.value.multi?.run?.finished == false) return
        multiJob?.cancel()
        _editor.update { it.copy(multi = null) }
    }

    /**
     * Leaves a finished multi-node run. Every node done: back to this node's config, read
     * again. Failed: back to the draft, as after a single apply.
     */
    fun finishMulti() {
        val run = _editor.value.multi?.run ?: return
        if (!run.finished) return
        if (run.error == null) {
            _editor.update { ConfigEditorState(tree = it.tree, treeStale = true) }
            refresh(reset = true)
        } else {
            _editor.update { it.copy(multi = null, review = null) }
        }
    }

    private fun updateMulti(change: (MultiConfigState) -> MultiConfigState) {
        _editor.update { state -> state.multi?.let { state.copy(multi = change(it)) } ?: state }
    }

    fun keep() = tries.keep()

    fun revertNow() = tries.revert()

    /**
     * Leaves a finished try. Kept or reverted: back to the node's config, read again. Failed:
     * back to the draft, which is still there to fix or try again.
     */
    fun finishTry() {
        val run = _editor.value.run
        if (run == null || run is ConfigTryState.Running) return
        tries.dismiss()
        when (run) {
            is ConfigTryState.Running -> Unit
            is ConfigTryState.Failed -> _editor.update { it.copy(run = null, review = null) }
            ConfigTryState.Kept, ConfigTryState.Reverted -> {
                _editor.update { ConfigEditorState(tree = it.tree, treeStale = true) }
                refresh(reset = true)
            }
        }
    }

    private fun currentText(): String? = _editor.value.draft ?: (state.value as? UiState.Loaded)?.data

    private suspend fun describe(text: String) {
        val revealed = _revealed.value
        val tree = try {
            talos.machineConfigDescribe(text, schemaVersion)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            ConfigTree(error = ConfigSyntaxError(message = e.userMessage()))
        }
        // A newer text may have come meanwhile: only the tree of what is shown is kept, and
        // never the tree of a revealed config once its secrets were hidden again.
        _editor.update {
            val current = if (it.draft == null) _revealed.value == revealed else it.draft == text
            if (current) it.copy(tree = tree, treeStale = false) else it
        }
    }
}
