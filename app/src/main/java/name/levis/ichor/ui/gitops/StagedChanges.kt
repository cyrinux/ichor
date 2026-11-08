package name.levis.ichor.ui.gitops

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.StagedEdits
import name.levis.ichor.data.StagedFile
import name.levis.ichor.data.StagedKey
import name.levis.ichor.data.stagedDefaultsPath
import name.levis.ichor.data.stagedKeyOf
import name.levis.ichor.model.DiffLine
import name.levis.ichor.model.GitProposal
import name.levis.ichor.model.GitProposalDefaults
import name.levis.ichor.model.GitProposalRecord
import name.levis.ichor.model.GitProposalResult
import name.levis.ichor.model.GitSource
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.uiText

/** A staged file on the Staged changes sheet, with its lines [added] and [removed] (null until counted). */
data class StagedRow(val file: StagedFile, val added: Int? = null, val removed: Int? = null)

/**
 * The Staged changes sheet: [open], its [rows], the one pull request [form] for all of them
 * (kept when its sheet closes: [formOpen]) and its [result].
 */
data class StagedChangesState(
    val open: Boolean = false,
    val rows: List<StagedRow> = emptyList(),
    val form: ProposeForm? = null,
    val formOpen: Boolean = false,
    val result: GitProposalResult? = null,
    /** Shown above the button: what was left out of the pull request and why (the Updates screen's skipped rows). */
    val notes: List<String> = emptyList(),
)

/**
 * The files staged in the cluster repository's browser ([attach]ed to its source): listed with
 * their line counts, removed one by one, then proposed as one pull request, after which they are
 * cleared and the request is remembered like the editor's.
 */
class StagedChanges(private val gitOps: GitOpsRepository, private val staged: StagedEdits, private val scope: CoroutineScope) {
    private val _state = MutableStateFlow(StagedChangesState())
    val state: StateFlow<StagedChangesState> = _state.asStateFlow()
    private var source: GitSource? = null
    private var countJob: Job? = null

    private val key: StagedKey? get() = source?.let(::stagedKeyOf)

    /** The repository on screen: its staged files are the sheet's. */
    fun attach(src: GitSource) {
        if (source?.let(::stagedKeyOf) != stagedKeyOf(src)) _state.value = StagedChangesState()
        source = src
    }

    /**
     * Stages [files] for the repository [src] (replacing earlier stages of the same paths) and
     * opens the sheet with the pull request [form] prefilled and [notes] above its button.
     */
    fun stageAndOpen(src: GitSource, files: List<StagedFile>, form: ProposeForm, notes: List<String>) {
        attach(src)
        val k = key ?: return
        files.forEach { staged.stage(k, it) }
        _state.update { it.copy(form = form, notes = notes) }
        open()
    }

    fun open() {
        _state.update { it.copy(open = true, result = null) }
        count()
    }

    fun close() = _state.update { if (it.form?.sending == true) it else it.copy(open = false, formOpen = false, result = null) }

    /** Drops [path] from the pull request; the sheet closes with the last one. */
    fun remove(path: String) {
        val k = key ?: return
        staged.remove(k, path)
        if (staged.of(k).isEmpty()) _state.value = StagedChangesState() else count()
    }

    /** Lists the staged files, keeping the counts already made, and counts the others' lines. */
    private fun count() {
        val k = key ?: return
        val files = staged.of(k)
        val known = _state.value.rows.associateBy { it.file }
        _state.update { s -> s.copy(rows = files.map { known[it] ?: StagedRow(it) }) }
        countJob?.cancel()
        countJob = scope.launch {
            files.filter { known[it]?.added == null }.forEach { f ->
                val lines = cancellableCatching { gitOps.textDiff(f.original, f.draft, f.path) }.getOrNull()?.resources?.firstOrNull()?.lines
                    ?: return@forEach
                val row = StagedRow(f, lines.count { it.kind == DiffLine.Kind.ADDED }, lines.count { it.kind == DiffLine.Kind.REMOVED })
                _state.update { s -> s.copy(rows = s.rows.map { if (it.file == f) row else it }) }
            }
        }
    }

    /** Opens the pull request sheet: as it was left, else prefilled after the files' common directory. */
    fun openForm() {
        val src = source ?: return
        val k = key ?: return
        if (staged.of(k).isEmpty()) return
        if (_state.value.form != null) return _state.update { it.copy(formOpen = true) }
        scope.launch {
            val paths = staged.of(k).map { it.path }
            val d = cancellableCatching { gitOps.repoProposalDefaults(src.repoURL, stagedDefaultsPath(paths)) }.getOrDefault(GitProposalDefaults())
            // The files were read (and their shas taken) at the key's branch: the request goes there.
            val form = ProposeForm(d.title, d.message, d.body, d.branch, base = k.baseRef, baseFixed = true)
            _state.update { it.copy(form = form, formOpen = true) }
        }
    }

    fun closeForm() = _state.update { if (it.form?.sending == true) it else it.copy(formOpen = false) }

    fun changeForm(transform: (ProposeForm) -> ProposeForm) = _state.update { s ->
        s.copy(form = s.form?.let { transform(it).copy(error = null, stale = false) })
    }

    /** Opens one pull request with every staged file; they are cleared once it is open. */
    fun propose() {
        val src = source ?: return
        val k = key ?: return
        val form = _state.value.form ?: return
        val files = staged.of(k)
        if (form.sending || files.isEmpty()) return
        _state.update { it.copy(form = form.copy(sending = true, error = null, stale = false)) }
        scope.launch {
            val proposal = GitProposal(
                path = "",
                content = "",
                fileSha = "",
                baseRef = form.base.trim(),
                message = form.message,
                title = form.title,
                body = form.body,
                branch = form.branch,
                draft = form.draft,
                files = files.map { it.proposal },
            )
            cancellableCatching { gitOps.propose(src, proposal) }.fold(
                onSuccess = { result ->
                    gitOps.rememberProposal(stagedRecord(src, files, form.title, result))
                    staged.clear(k)
                    _state.value = StagedChangesState(open = true, result = result)
                },
                onFailure = { e -> _state.update { it.copy(form = it.form?.copy(sending = false, error = e.uiText())) } },
            )
        }
    }
}

/** The record kept of the pull request [result] opened with the staged [files] of the repository [src]. */
internal fun stagedRecord(src: GitSource, files: List<StagedFile>, title: String, result: GitProposalResult): GitProposalRecord = GitProposalRecord(
    repoURL = src.repoURL,
    path = files.joinToString(", ") { it.path },
    title = title,
    url = result.url,
    number = result.number,
    branch = result.branch,
    commit = result.commit,
    forge = result.forge,
    createdAt = System.currentTimeMillis(),
)
