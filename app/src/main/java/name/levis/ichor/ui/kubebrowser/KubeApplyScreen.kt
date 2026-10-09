package name.levis.ichor.ui.kubebrowser

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.KubeApplyResult
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.diff.DiffCounts
import name.levis.ichor.ui.diff.DiffResourceCard
import name.levis.ichor.ui.app
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/**
 * Pasted manifests, previewed then applied: the text, the namespace for objects that name
 * none, what a server-side apply dry run says each object would become, and the apply itself.
 * Editing the text drops the preview: what is applied is always what was previewed.
 */
class KubeApplyViewModel(private val browser: KubeBrowserRepository) : ViewModel() {
    data class State(
        val manifests: String = "",
        val namespace: String = "",
        /** The dry run of [manifests]; null before Preview, or after an edit. */
        val preview: UiState<KubeApplyResult>? = null,
        /** The apply; null until Apply. */
        val applied: UiState<KubeApplyResult>? = null,
    ) {
        val busy: Boolean get() = preview == UiState.Loading || applied == UiState.Loading
        val previewed: KubeApplyResult? get() = (preview as? UiState.Loaded)?.data
        val canApply: Boolean get() = !busy && applied !is UiState.Loaded && previewed?.hasChanges == true
    }

    private val _state = MutableStateFlow(State())
    val state: StateFlow<State> = _state.asStateFlow()
    private var job: Job? = null

    fun setManifests(text: String) {
        job?.cancel()
        _state.update { it.copy(manifests = text, preview = null, applied = null) }
    }

    fun setNamespace(namespace: String) {
        job?.cancel()
        _state.update { it.copy(namespace = namespace.trim(), preview = null, applied = null) }
    }

    fun preview() = run(
        start = { it.copy(preview = UiState.Loading, applied = null) },
        work = { browser.applyPreview(it.namespace, it.manifests) },
        done = { s, r -> s.copy(preview = r.fold({ UiState.Loaded(it) }, { UiState.Failed(it.uiText()) })) },
    )

    fun apply() = run(
        start = { it.copy(applied = UiState.Loading) },
        work = { browser.apply(it.namespace, it.manifests) },
        done = { s, r -> s.copy(applied = r.fold({ UiState.Loaded(it) }, { UiState.Failed(it.uiText()) })) },
    )

    private fun run(start: (State) -> State, work: suspend (State) -> KubeApplyResult, done: (State, Result<KubeApplyResult>) -> State) {
        job?.cancel()
        val input = _state.updateAndGet(start)
        job = viewModelScope.launch {
            val result = cancellableCatching { work(input) }
            _state.update { done(it, result) }
        }
    }

    private fun MutableStateFlow<State>.updateAndGet(f: (State) -> State): State {
        update(f)
        return value
    }
}

/** Unfolded at first: the first objects that would change. */
private const val UNFOLDED_AT_FIRST = 3

/**
 * Apply YAML from the clipboard (a snippet from a chat, a runbook, a Gist), like
 * `kubectl apply --server-side`: paste, preview the diff of each object, then apply. A field
 * another manager owns is refused with its name, never taken over.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeApplyScreen(onBack: () -> Unit, vm: KubeApplyViewModel = viewModel(factory = factory { KubeApplyViewModel(app.kubeBrowser) })) {
    val state by vm.state.collectAsStateWithLifecycle()
    val clipboard = LocalClipboard.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val expanded = remember { mutableStateMapOf<String, Boolean>() }

    Scaffold(
        topBar = { TopAppBar(title = { Text(stringResource(R.string.kb_apply_title)) }, navigationIcon = { BackButton(onBack) }) },
    ) { padding ->
        LazyColumn(Modifier.pageContent(padding).fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item(key = "input") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    MutedText(stringResource(R.string.kb_apply_hint))
                    OutlinedTextField(
                        value = state.manifests,
                        onValueChange = vm::setManifests,
                        label = { Text(stringResource(R.string.kb_apply_manifests)) },
                        textStyle = TextStyle(fontFamily = FontFamily.Monospace, fontSize = MaterialTheme.typography.bodySmall.fontSize),
                        minLines = 8,
                        maxLines = 24,
                        enabled = !state.busy,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        OutlinedButton(onClick = { scope.launch { readClipboard(clipboard, context)?.let(vm::setManifests) } }, enabled = !state.busy) {
                            Icon(Icons.Outlined.ContentPaste, contentDescription = null, modifier = Modifier.size(18.dp))
                            Text(stringResource(R.string.kb_apply_paste), modifier = Modifier.padding(start = 6.dp))
                        }
                        OutlinedTextField(
                            value = state.namespace,
                            onValueChange = vm::setNamespace,
                            label = { Text(stringResource(R.string.kb_apply_namespace)) },
                            singleLine = true,
                            enabled = !state.busy,
                            modifier = Modifier.weight(1f),
                        )
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = vm::preview, enabled = !state.busy && state.manifests.isNotBlank(), modifier = Modifier.weight(1f)) {
                            Text(stringResource(R.string.kb_apply_preview))
                        }
                        Button(onClick = vm::apply, enabled = state.canApply, modifier = Modifier.weight(1f)) {
                            Icon(Icons.Outlined.PlayArrow, contentDescription = null, modifier = Modifier.size(18.dp))
                            Text(stringResource(R.string.kb_apply_apply), modifier = Modifier.padding(start = 6.dp))
                        }
                    }
                }
            }
            val shown = state.applied ?: state.preview
            when (shown) {
                null -> {}
                UiState.Loading -> item(key = "loading") {
                    Row(Modifier.fillMaxWidth().padding(vertical = 16.dp), horizontalArrangement = Arrangement.Center) { CircularProgressIndicator() }
                }
                is UiState.Failed -> item(key = "failed") { ErrorBox(shown.message, if (state.applied != null) vm::apply else vm::preview) }
                is UiState.Loaded -> resultItems(shown.data, applied = state.applied != null, expanded)
            }
            item(key = "footnote") {
                Text(stringResource(R.string.kb_apply_footnote), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.resultItems(result: KubeApplyResult, applied: Boolean, expanded: MutableMap<String, Boolean>) {
    item(key = "summary") {
        val colors = LocalStatusColors.current
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (applied) {
                Text(
                    pluralStringResource(R.plurals.kb_apply_done, result.applied, result.applied),
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (result.failed == 0) colors.ok else colors.warn,
                )
            } else if (!result.hasChanges && result.failed == 0) {
                Text(stringResource(R.string.kb_apply_in_sync), style = MaterialTheme.typography.bodyMedium, color = colors.ok)
            }
            if (result.failed > 0) {
                Text(pluralStringResource(R.plurals.kb_apply_refused, result.failed, result.failed), style = MaterialTheme.typography.bodyMedium, color = colors.bad)
            }
            DiffCounts(result.counts)
            result.warnings.forEach { Text(it, style = MaterialTheme.typography.bodySmall, color = colors.warn) }
        }
    }
    itemsIndexed(result.resources, key = { _, r -> r.key }) { index, r ->
        val open = expanded[r.key] ?: (index < UNFOLDED_AT_FIRST && (r.change.isChange || r.error.isNotEmpty()))
        DiffResourceCard(r, open) { expanded[r.key] = !open }
    }
}

/** The clipboard's text, null when it holds none. */
private suspend fun readClipboard(clipboard: androidx.compose.ui.platform.Clipboard, context: Context): String? {
    val clip = clipboard.getClipEntry()?.clipData ?: return null
    if (clip.itemCount == 0) return null
    return clip.getItemAt(0).coerceToText(context)?.toString()?.takeIf { it.isNotBlank() }
}
