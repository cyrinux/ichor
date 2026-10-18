package name.levis.ichor.ui.gitops

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.GitHostMissing
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.DEFAULT_PROPOSAL_BASE
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.GitProposal
import name.levis.ichor.model.GitProposalDefaults
import name.levis.ichor.model.GitProposalResult
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ManifestType
import name.levis.ichor.model.ProposeBlock
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.model.isStaleFileError
import name.levis.ichor.model.manifestType
import name.levis.ichor.model.proposalBaseRef
import name.levis.ichor.model.quickEditTitle
import name.levis.ichor.model.quickProposalDefaults
import name.levis.ichor.model.yamlCursorAt
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.kubebrowser.SchemaHelp
import name.levis.ichor.ui.kubebrowser.SchemaHelpMode
import name.levis.ichor.ui.uiText

/**
 * The pull request being written: prefilled from `GitOpsProposalDefaults`. [base] is the
 * branch it goes to, editable only when the source does not follow a branch ([baseFixed]
 * false); [sending] while the forge works, [error] when it refused, [stale] when the file
 * changed upstream (Reload).
 */
data class ProposeForm(
    val title: String,
    val message: String,
    val body: String,
    val branch: String,
    val base: String,
    val baseFixed: Boolean,
    val draft: Boolean = false,
    val sending: Boolean = false,
    val error: UiText? = null,
    val stale: Boolean = false,
)

/**
 * An edit of one source file: [original] as the forge has it at [readRef] (its [fileSha] is
 * the proposal's lock), the [draft] typed, then the [review] diff, the [form] of the pull
 * request (kept when its sheet closes: [formOpen]) and its [result]. A quick edit leaves its
 * [quick] draft and title (the request's default while the draft is still that one), or a
 * [notice] when it did not apply to the file read here (or, kept live values, which it left).
 */
data class SourceEdit(
    val source: GitSource,
    val path: String,
    val readRef: String,
    val original: String,
    val fileSha: String,
    val draft: String,
    val manifest: ManifestType?,
    val defaults: GitProposalDefaults,
    val review: UiState<FluxDiff>? = null,
    val form: ProposeForm? = null,
    val formOpen: Boolean = false,
    val result: GitProposalResult? = null,
    val reloading: Boolean = false,
    val quick: QuickDraft? = null,
    val notice: UiText? = null,
) {
    val changed: Boolean get() = draft != original
}

/** A quick edit's result: the [draft] it made and the pull request [title] it names. */
data class QuickDraft(val draft: String, val title: String)

/** The editor screen: the file being read, or why it cannot be edited ([block]). */
data class SourceEditState(val edit: UiState<SourceEdit> = UiState.Loading, val block: ProposeBlock? = null)

/**
 * Edits one file of an app's source and proposes it as a pull request: the file is read through
 * the forge at the branch the request goes to (the deployed commit when the app does not follow
 * a branch), reviewed as a text diff, then committed on a new branch by the Go core. The draft
 * lives here, never in the route.
 */
class SourceEditViewModel(private val gitOps: GitOpsRepository, private val browser: KubeBrowserRepository) : ViewModel() {
    private val _state = MutableStateFlow(SourceEditState())
    val state: StateFlow<SourceEditState> = _state.asStateFlow()
    private val _help = MutableStateFlow<SchemaHelp?>(null)
    val help: StateFlow<SchemaHelp?> = _help.asStateFlow()
    private var key: Any? = null
    private var target: SourceTarget? = null
    private var path: String = ""
    private var quick: QuickEdit? = null
    private var job: Job? = null
    private var helpLoad: Job? = null

    /** Reads [path] of [target] once per [key]; a [quick] edit is applied to it and reviewed at once. */
    fun load(target: SourceTarget, path: String, key: Any, quick: QuickEdit? = null) {
        if (key == this.key) return
        this.key = key
        this.target = target
        this.path = path
        this.quick = quick
        refresh()
    }

    /** Reads the file again; not once an edit is open (its draft would be lost). */
    fun refresh() {
        val t = target ?: return
        if (_state.value.edit is UiState.Loaded) return
        job?.cancel()
        _state.value = SourceEditState()
        job = viewModelScope.launch {
            _state.value = try {
                open(t)
            } catch (e: CancellationException) {
                throw e
            } catch (e: GitHostMissing) {
                SourceEditState(UiState.Loading, ProposeBlock.NoToken(e.host))
            } catch (e: Throwable) {
                SourceEditState(UiState.Failed(e.uiText()))
            }
        }
    }

    private suspend fun open(t: SourceTarget): SourceEditState {
        val src = gitOps.sourceOf(t)
        gitOps.proposeBlockOf(src)?.let { return SourceEditState(UiState.Loading, it) }
        val readRef = proposalBaseRef(src.tracked) ?: src.ref
        val file = gitOps.forgeFile(src, readRef, path)
        if (file.truncated) throw LocalizedException(UiText.Res(R.string.gitops_source_truncated))
        val defaults = cancellableCatching { gitOps.proposalDefaults(t.tool, t.kind, t.namespace, t.name, path) }.getOrDefault(GitProposalDefaults())
        val edit = SourceEdit(
            source = src,
            path = path,
            readRef = readRef,
            original = file.content,
            fileSha = file.sha,
            draft = file.content,
            manifest = manifestType(path, file.content),
            defaults = defaults,
        )
        val q = quick
        return SourceEditState(UiState.Loaded(if (q != null) withQuickEdit(edit, q) else edit))
    }

    /**
     * [edit] with [quick] applied to the file as read here (the branch the request goes to may
     * differ from the deployed file shown before), straight on its review; when the branch
     * already has the value or the core refuses, the editor opens on the file with why.
     */
    private suspend fun withQuickEdit(edit: SourceEdit, quick: QuickEdit): SourceEdit {
        val outcome = cancellableCatching { gitOps.quickEdit(edit.original, quick) }
        val result = outcome.getOrElse { return edit.copy(notice = it.uiText()) }
        if (!result.changed) return edit.copy(notice = UiText.Res(R.string.gitops_quick_unchanged))
        val review = cancellableCatching { gitOps.textDiff(edit.original, result.content, edit.path) }
            .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
        // Live values the file does not set (an overlay or a patch does) are named on the review.
        val skipped = result.skipped.takeIf { it.isNotEmpty() }?.let { UiText.Res(R.string.gitops_keep_skipped, it.joinToString("; ")) }
        return edit.copy(draft = result.content, review = review, quick = QuickDraft(result.content, quickEditTitle(quick)), notice = skipped)
    }

    private fun updateEdit(transform: (SourceEdit) -> SourceEdit) = _state.update { s ->
        val loaded = s.edit as? UiState.Loaded ?: return@update s
        s.copy(edit = loaded.copy(data = transform(loaded.data)))
    }

    private val current: SourceEdit? get() = (_state.value.edit as? UiState.Loaded)?.data

    fun changeDraft(text: String) = updateEdit { it.copy(draft = text, notice = null) }

    /** Shows what the draft changes, as a unified diff. */
    fun review() {
        val edit = current ?: return
        job?.cancel()
        updateEdit { it.copy(review = UiState.Loading) }
        job = viewModelScope.launch {
            val result = cancellableCatching { gitOps.textDiff(edit.original, edit.draft, edit.path) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
            updateEdit { it.copy(review = result) }
        }
    }

    fun backToEditor() {
        job?.cancel()
        updateEdit { it.copy(review = null, formOpen = false) }
    }

    /** Opens the pull request sheet: as it was left, else prefilled. */
    fun openForm() {
        val edit = current ?: return
        if (edit.form != null) return updateEdit { it.copy(formOpen = true) }
        val fixed = proposalBaseRef(edit.source.tracked)
        val d = quickProposalDefaults(edit.defaults, edit.quick?.draft, edit.quick?.title, edit.draft)
        val form = ProposeForm(
            title = d.title,
            message = d.message,
            body = d.body,
            branch = d.branch,
            base = fixed ?: DEFAULT_PROPOSAL_BASE,
            baseFixed = fixed != null,
        )
        updateEdit { it.copy(form = form, formOpen = true) }
    }

    fun closeForm() = updateEdit { if (it.form?.sending == true || it.reloading) it else it.copy(formOpen = false) }

    fun changeForm(transform: (ProposeForm) -> ProposeForm) = updateEdit { e ->
        e.copy(form = e.form?.let { transform(it).copy(error = null, stale = false) })
    }

    /**
     * Opens the pull request. The file's sha is the one read; when the request goes to another
     * branch than the one read (the source follows a tag or a commit), the file is read there:
     * its sha is taken only when the text is the same, else the forge refuses the stale file.
     */
    fun propose() {
        val edit = current ?: return
        val form = edit.form ?: return
        if (form.sending) return
        updateEdit { it.copy(form = form.copy(sending = true, error = null, stale = false)) }
        viewModelScope.launch {
            val outcome = cancellableCatching {
                val base = form.base.trim()
                val sha = if (base == edit.readRef) edit.fileSha else shaAt(edit, base)
                gitOps.propose(
                    edit.source,
                    GitProposal(
                        path = edit.path,
                        content = edit.draft,
                        fileSha = sha,
                        baseRef = base,
                        message = form.message,
                        title = form.title,
                        body = form.body,
                        branch = form.branch,
                        draft = form.draft,
                    ),
                )
            }
            outcome.fold(
                onSuccess = { result -> updateEdit { it.copy(formOpen = false, review = null, result = result, original = it.draft) } },
                onFailure = { e ->
                    updateEdit { it.copy(form = it.form?.copy(sending = false, error = e.uiText(), stale = isStaleFileError(e.message))) }
                },
            )
        }
    }

    private suspend fun shaAt(edit: SourceEdit, base: String): String {
        val there = gitOps.forgeFile(edit.source, base, edit.path)
        return if (there.content == edit.original) there.sha else edit.fileSha
    }

    /**
     * Reads the file again at the branch the request goes to, after the forge said it changed:
     * the new text becomes the original (so the review shows what the draft does to it) and its
     * sha the lock; the draft is kept and reviewed again.
     */
    fun reload() {
        val edit = current ?: return
        if (edit.reloading) return
        val base = edit.form?.base?.trim().orEmpty().ifEmpty { edit.readRef }
        updateEdit { it.copy(reloading = true) }
        viewModelScope.launch {
            val outcome = cancellableCatching { gitOps.forgeFile(edit.source, base, edit.path) }
            outcome.fold(
                onSuccess = { file ->
                    // The draft was made from the old text: proposing it would quietly undo the
                    // upstream change, so start again from the new text (as on iOS).
                    updateEdit {
                        it.copy(
                            readRef = base,
                            original = file.content,
                            fileSha = file.sha,
                            draft = file.content,
                            manifest = manifestType(it.path, file.content),
                            review = null,
                            form = it.form?.copy(error = null, stale = false),
                            formOpen = false,
                            reloading = false,
                        )
                    }
                },
                onFailure = { e -> updateEdit { it.copy(reloading = false, form = it.form?.copy(error = e.uiText(), stale = false)) } },
            )
        }
    }

    /** Opens the schema help for the cursor at [offset] of [text] (the editor's own, ahead of the draft). */
    fun openHelp(text: String, offset: Int, mode: SchemaHelpMode) {
        val help = SchemaHelp(mode, yamlCursorAt(text, offset), offset)
        _help.value = help
        loadHelp(help)
    }

    fun retryHelp() {
        val help = _help.value ?: return
        _help.value = help.copy(state = UiState.Loading)
        loadHelp(help)
    }

    fun closeHelp() {
        helpLoad?.cancel()
        _help.value = null
    }

    private fun loadHelp(help: SchemaHelp) {
        val type = current?.manifest ?: return
        // Only the kind's schema is read: the object itself is not in the cluster.
        val ref = KubeObjectRef(type.group, type.version, resource = "", kind = type.kind, namespace = "", name = "", editable = false)
        helpLoad?.cancel()
        helpLoad = viewModelScope.launch {
            val result = cancellableCatching { browser.explain(ref, help.path) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
            _help.update { if (it?.path == help.path && it.mode == help.mode) it.copy(state = result) else it }
        }
    }
}
