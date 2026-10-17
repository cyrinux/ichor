package name.levis.ichor.ui.flux

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
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
import name.levis.ichor.model.GitOpsSourceFind
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.KubeDiffResource
import name.levis.ichor.model.ManifestMatch
import name.levis.ichor.model.findableSourceIndex
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.gitops.SOURCE_TOOL_ARGO
import name.levis.ichor.ui.gitops.SOURCE_TOOL_FLUX
import name.levis.ichor.ui.uiText

/** The `GitOpsSource` tool name of a diff's tool. */
val DiffTool.sourceTool: String get() = if (this == DiffTool.ARGO) SOURCE_TOOL_ARGO else SOURCE_TOOL_FLUX

/** What "Open source file" ends with. */
sealed interface SourceFindOutcome {
    /** One manifest: open the file [path] of source [index] at [line]. */
    data class Open(val index: Int, val path: String, val line: Int) : SourceFindOutcome

    /** Several manifests declare it: the user picks one. */
    data class Choose(val index: Int, val matches: List<ManifestMatch>) : SourceFindOutcome

    /** Nothing to open: [text] says why; [addToken]: no token is saved for the Git host. */
    data class Message(val text: UiText, val addToken: Boolean = false) : SourceFindOutcome
}

/**
 * What the search of [resource] in source [index] gives: the file to open, a choice, or why there
 * is none (the core's note when it has one, e.g. a Helm-rendered app).
 */
fun sourceFindOutcome(index: Int, result: GitOpsSourceFind, resource: KubeDiffResource): SourceFindOutcome {
    val matches = result.matches.filter { it.path.isNotEmpty() }
    return when {
        matches.size == 1 -> SourceFindOutcome.Open(index, matches.single().path, matches.single().line)
        matches.size > 1 -> SourceFindOutcome.Choose(index, matches)
        result.note.isNotBlank() -> SourceFindOutcome.Message(UiText.Raw(result.note))
        else -> SourceFindOutcome.Message(UiText.Res(R.string.gitops_find_none, resource.kind, resource.name))
    }
}

/**
 * "Open source file" on a GitOps diff: the app's first readable Git source is read once per
 * [load] key ([sourceIndex] stays null when it has none: the action is not offered), then each
 * object is looked up in it on demand, one search at a time.
 */
class DiffSourceViewModel(
    private val gitOps: GitOpsRepository,
    private val tool: String,
    private val kind: String,
    private val namespace: String,
    private val name: String,
) : ViewModel() {
    private val _sourceIndex = MutableStateFlow<Int?>(null)
    val sourceIndex: StateFlow<Int?> = _sourceIndex.asStateFlow()
    private val _searching = MutableStateFlow<String?>(null)

    /** The key of the object being searched, null when none is. */
    val searching: StateFlow<String?> = _searching.asStateFlow()
    private val _outcome = MutableStateFlow<SourceFindOutcome?>(null)

    /** The last search's result, until [consumed]. */
    val outcome: StateFlow<SourceFindOutcome?> = _outcome.asStateFlow()
    private var source: GitSource? = null
    private var key: Any? = null
    private var loadJob: Job? = null
    private var findJob: Job? = null

    fun load(key: Any) {
        if (key == this.key) return
        this.key = key
        loadJob?.cancel()
        findJob?.cancel()
        _searching.value = null
        loadJob = viewModelScope.launch {
            val sources = cancellableCatching { gitOps.source(tool, kind, namespace, name) }.getOrNull()
            val index = sources?.let { findableSourceIndex(it) }
            source = index?.let { sources?.sources?.getOrNull(it) }
            _sourceIndex.value = index
        }
    }

    fun find(resource: KubeDiffResource) {
        val index = _sourceIndex.value ?: return
        val src = source ?: return
        if (_searching.value != null) return
        _searching.value = resource.key
        findJob = viewModelScope.launch {
            try {
                val result = gitOps.findSource(tool, kind, namespace, name, index, src, resource)
                _outcome.value = sourceFindOutcome(index, result, resource)
            } catch (e: CancellationException) {
                throw e
            } catch (e: GitHostMissing) {
                _outcome.value = SourceFindOutcome.Message(e.uiText(), addToken = true)
            } catch (e: Throwable) {
                _outcome.value = SourceFindOutcome.Message(e.uiText())
            } finally {
                _searching.value = null
            }
        }
    }

    fun consumed() {
        _outcome.value = null
    }
}

/** The manifests that declare an object, one row each ("Defined in path, line n"); a tap opens it. */
@Composable
fun SourceMatchChooser(matches: List<ManifestMatch>, onPick: (ManifestMatch) -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.AutoMirrored.Outlined.InsertDriveFile, contentDescription = null) },
        title = { Text(stringResource(R.string.gitops_find_choose)) },
        text = {
            Column {
                matches.forEach { m ->
                    Text(
                        stringResource(R.string.gitops_find_defined_in, m.path, m.line.toString()),
                        style = MaterialTheme.typography.bodyMedium,
                        fontFamily = FontFamily.Monospace,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable(role = Role.Button) { onPick(m) }
                            .padding(vertical = 12.dp),
                    )
                }
            }
        },
        confirmButton = {},
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
