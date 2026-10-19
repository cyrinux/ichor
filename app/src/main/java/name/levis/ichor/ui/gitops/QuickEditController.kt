package name.levis.ichor.ui.gitops

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.uiText

/** Which quick edit sheet is open. */
enum class QuickSheetKind { IMAGE, REPLICAS }

/**
 * What the image sheet suggests: the app's [liveImages] (Argo CD's list) and the kept
 * [revisions] of a document's workload by its index (none have a source yet); empty for Flux
 * or when the cluster cannot be read.
 */
data class QuickSuggestions(
    val liveImages: List<String> = emptyList(),
    val revisions: Map<Int, List<KubeRevision>> = emptyMap(),
)

/** An open quick edit sheet: [applying] while the core rewrites the file, [error] when it refused or nothing changed. */
data class QuickSheet(
    val kind: QuickSheetKind,
    val suggestions: QuickSuggestions = QuickSuggestions(),
    val applying: Boolean = false,
    val error: UiText? = null,
)

/**
 * The quick edits of the file on screen ([content]): opens a sheet, loads the image sheet's
 * suggestions, and checks an edit on the file before [applied] hands it to the editor (which
 * applies it again to the file as the pull request's branch has it).
 */
class QuickEditController(
    private val gitOps: GitOpsRepository,
    private val scope: CoroutineScope,
    private val content: () -> String?,
) {
    private val _sheet = MutableStateFlow<QuickSheet?>(null)
    val sheet: StateFlow<QuickSheet?> = _sheet.asStateFlow()
    private val _applied = MutableStateFlow<QuickEdit?>(null)

    /** The edit checked on the file: the screen opens the editor's review with it, then [consumeApplied]. */
    val applied: StateFlow<QuickEdit?> = _applied.asStateFlow()

    /** The app whose file is shown. */
    var target: SourceTarget? = null
    private var suggestions: QuickSuggestions? = null
    private var job: Job? = null

    fun open(kind: QuickSheetKind) {
        _sheet.value = QuickSheet(kind, suggestions ?: QuickSuggestions())
        if (kind == QuickSheetKind.IMAGE && suggestions == null) loadSuggestions()
    }

    fun close() {
        if (_sheet.value?.applying == true) return
        job?.cancel()
        _sheet.value = null
    }

    fun consumeApplied() {
        _applied.value = null
    }

    /** Rewrites the file with [edit] off the main thread: unchanged says so, else [applied] carries it. */
    fun applyEdit(edit: QuickEdit) {
        val text = content() ?: return
        val current = _sheet.value ?: return
        if (current.applying) return
        _sheet.value = current.copy(applying = true, error = null)
        job = scope.launch {
            cancellableCatching { gitOps.quickEdit(text, edit) }.fold(
                onSuccess = { result ->
                    if (result.changed) {
                        _sheet.value = null
                        _applied.value = edit
                    } else {
                        _sheet.update { it?.copy(applying = false, error = UiText.Res(R.string.gitops_quick_unchanged)) }
                    }
                },
                onFailure = { e -> _sheet.update { it?.copy(applying = false, error = e.uiText()) } },
            )
        }
    }

    /**
     * Argo CD only: the app's live images ("Running now"). Its history keeps no image list, so
     * the "Previous tag" chip has no source yet: [QuickSuggestions.revisions] stays empty.
     */
    private fun loadSuggestions() {
        val t = target as? SourceTarget.App ?: return
        if (t.tool != SOURCE_TOOL_ARGO) return
        scope.launch {
            val app = cancellableCatching { gitOps.argoCD().apps.firstOrNull { it.namespace == t.namespace && it.name == t.name } }.getOrNull()
                ?: return@launch
            val loaded = QuickSuggestions(liveImages = app.images)
            suggestions = loaded
            _sheet.update { it?.copy(suggestions = loaded) }
        }
    }
}
