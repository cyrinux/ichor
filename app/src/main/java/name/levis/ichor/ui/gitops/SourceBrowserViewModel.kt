package name.levis.ichor.ui.gitops

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.GitHostMissing
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.model.GitFile
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.GitTree
import name.levis.ichor.model.ProposeBlock
import name.levis.ichor.model.QuickEditTargets
import name.levis.ichor.model.isYamlPath
import name.levis.ichor.model.SourceReadPath
import name.levis.ichor.model.cleanSourceDir
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.refreshFailed
import name.levis.ichor.ui.uiText

/** The app whose source is browsed: [index] picks the source among `GitOpsSource`'s. */
data class SourceTarget(val tool: String, val kind: String, val namespace: String, val name: String, val index: Int)

/** The directory on screen; [missingHost]: no token is saved for that Git host. */
data class SourceListing(
    val dir: String = "",
    val tree: UiState<GitTree> = UiState.Loading,
    val missingHost: String? = null,
)

/** The source [index] of [target]'s, or a localized error when the app has no such source. */
internal suspend fun GitOpsRepository.sourceOf(target: SourceTarget): GitSource =
    source(target.tool, target.kind, target.namespace, target.name).sources.getOrNull(target.index)
        ?: throw LocalizedException(UiText.Res(R.string.gitops_source_not_browsable))

/**
 * The source browser: the app's source read once per [load] key (the context and config
 * generation), then one directory at a time. The directories walked are kept, so back goes up
 * the way the user came down before leaving the screen.
 */
class SourceBrowserViewModel(private val gitOps: GitOpsRepository) : ViewModel() {
    private val _source = MutableStateFlow<UiState<GitSource>>(UiState.Loading)
    val source: StateFlow<UiState<GitSource>> = _source.asStateFlow()
    private val _listing = MutableStateFlow(SourceListing())
    val listing: StateFlow<SourceListing> = _listing.asStateFlow()
    private val _trail = MutableStateFlow<List<String>>(emptyList())

    /** The directories back goes to, the last one first. */
    val trail: StateFlow<List<String>> = _trail.asStateFlow()
    private var key: Any? = null
    private var target: SourceTarget? = null
    private var sourceJob: Job? = null
    private var listJob: Job? = null

    /** [startDir]: where to open, null for the source's own path. */
    fun load(target: SourceTarget, startDir: String?, key: Any) {
        if (key == this.key) return
        this.key = key
        this.target = target
        _trail.value = emptyList()
        fetchSource(startDir)
    }

    /** Opens [dir], remembering the one on screen for back. */
    fun open(dir: String) {
        val clean = cleanSourceDir(dir)
        val current = _listing.value.dir
        if (clean == current) return
        _trail.value = _trail.value + current
        list(clean)
    }

    /** Back to the directory before; false when there is none (back leaves the screen). */
    fun back(): Boolean {
        val trail = _trail.value
        if (trail.isEmpty()) return false
        _trail.value = trail.dropLast(1)
        list(trail.last())
        return true
    }

    fun refresh() {
        if (_source.value is UiState.Loaded) list(_listing.value.dir) else fetchSource(null)
    }

    private fun fetchSource(startDir: String?) {
        val t = target ?: return
        sourceJob?.cancel()
        listJob?.cancel()
        sourceJob = viewModelScope.launch {
            try {
                val src = gitOps.sourceOf(t)
                _source.value = UiState.Loaded(src)
                if (src.readPath != SourceReadPath.NONE) list(cleanSourceDir(startDir ?: src.path))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _source.value = _source.value.refreshFailed(e.uiText())
            }
        }
    }

    private fun list(dir: String) {
        val src = (_source.value as? UiState.Loaded)?.data ?: return
        listJob?.cancel()
        val previous = _listing.value
        _listing.value = if (previous.dir == dir && previous.tree is UiState.Loaded) {
            previous.copy(tree = previous.tree.copy(refreshing = true))
        } else {
            SourceListing(dir)
        }
        listJob = viewModelScope.launch {
            _listing.value = try {
                SourceListing(dir, UiState.Loaded(gitOps.tree(src, dir)))
            } catch (e: CancellationException) {
                throw e
            } catch (e: GitHostMissing) {
                SourceListing(dir, UiState.Loading, missingHost = e.host)
            } catch (e: Throwable) {
                SourceListing(dir, _listing.value.tree.refreshFailed(e.uiText()))
            }
        }
    }
}

/** A file of a source, with the source it was read from (for its links). */
data class SourceFileView(val source: GitSource, val file: GitFile)

/**
 * The file on screen; [missingHost]: no token is saved for that Git host. [editBlock]: why
 * Edit is disabled (no token to propose with, no forge), null when it is offered.
 */
data class SourceFileState(
    val file: UiState<SourceFileView> = UiState.Loading,
    val missingHost: String? = null,
    val editBlock: ProposeBlock? = null,
    /** What the file offers to quick edits, null when nothing (or not a YAML file). */
    val quick: QuickEditTargets? = null,
)

/**
 * One file of an app's source, read once per [load] key, again on refresh; its quick edits
 * through [quick].
 */
class SourceFileViewModel(private val gitOps: GitOpsRepository) : ViewModel() {
    private val _state = MutableStateFlow(SourceFileState())
    val state: StateFlow<SourceFileState> = _state.asStateFlow()

    /** The quick edit sheets of the file on screen. */
    val quick = QuickEditController(gitOps, viewModelScope) { (_state.value.file as? UiState.Loaded)?.data?.file?.content }
    private var key: Any? = null
    private var target: SourceTarget? = null
    private var path: String = ""
    private var job: Job? = null

    fun load(target: SourceTarget, path: String, key: Any) {
        if (key == this.key) return
        this.key = key
        this.target = target
        this.path = path
        quick.target = target
        refresh()
    }

    fun refresh() {
        val t = target ?: return
        job?.cancel()
        val current = _state.value
        (current.file as? UiState.Loaded)?.let { _state.value = current.copy(file = it.copy(refreshing = true)) }
        job = viewModelScope.launch {
            _state.value = try {
                val src = gitOps.sourceOf(t)
                val file = gitOps.file(src, path)
                val block = cancellableCatching { gitOps.proposeBlockOf(src) }.getOrDefault(ProposeBlock.NotEditable)
                val quick = if (block == null && !file.truncated && isYamlPath(path)) {
                    cancellableCatching { gitOps.quickEditTargets(file.content) }.getOrNull()?.takeUnless { it.isEmpty }
                } else {
                    null
                }
                SourceFileState(UiState.Loaded(SourceFileView(src, file)), editBlock = block, quick = quick)
            } catch (e: CancellationException) {
                throw e
            } catch (e: GitHostMissing) {
                SourceFileState(missingHost = e.host)
            } catch (e: Throwable) {
                SourceFileState(_state.value.file.refreshFailed(e.uiText()))
            }
        }
    }
}
