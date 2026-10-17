package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.selection.triStateToggleable
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TriStateCheckbox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.state.ToggleableState
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubeSanitizePreview
import name.levis.ichor.model.KubeSanitizeResult
import name.levis.ichor.model.SanitizeGroup
import name.levis.ichor.model.SanitizePod
import name.levis.ichor.model.defaultChecked
import name.levis.ichor.model.groupedByStatus
import name.levis.ichor.model.sweepNames
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/**
 * The preview and the sweep of a k9s-style sanitize: loaded for one namespace (null: every
 * namespace), then the checked pods deleted at once. Kept in a ViewModel so a sweep in flight
 * finishes when the sheet closes.
 */
class SanitizeViewModel(private val kube: KubeRepository) : ViewModel() {
    private val _preview = MutableStateFlow<UiState<KubeSanitizePreview>>(UiState.Loading)
    val preview: StateFlow<UiState<KubeSanitizePreview>> = _preview.asStateFlow()

    private val _sweeping = MutableStateFlow(false)
    val sweeping: StateFlow<Boolean> = _sweeping.asStateFlow()

    private val _outcome = MutableStateFlow<Result<KubeSanitizeResult>?>(null)
    /** The last sweep's outcome, until the next load. */
    val outcome: StateFlow<Result<KubeSanitizeResult>?> = _outcome.asStateFlow()

    private var loading: Job? = null

    fun load(namespace: String?) {
        loading?.cancel()
        _preview.value = UiState.Loading
        loading = viewModelScope.launch {
            val read = cancellableCatching { kube.sanitizePreview(namespace.orEmpty()) }
            _preview.value = read.fold({ UiState.Loaded(it) }, { UiState.Failed(it.uiText()) })
        }
    }

    /** Deletes [names] (see [sweepNames]), then [onDone] with the outcome. */
    fun sweep(namespace: String?, names: String, onDone: (Result<KubeSanitizeResult>) -> Unit) {
        if (_sweeping.value || names.isEmpty()) return
        _sweeping.value = true
        viewModelScope.launch {
            val result = cancellableCatching { kube.sanitize(namespace.orEmpty(), names) }
            _sweeping.value = false
            _outcome.value = result
            onDone(result)
        }
    }

    fun clearOutcome() {
        _outcome.value = null
    }
}

/**
 * "Clean up dead pods" for [namespace] (null: every namespace): the pods k9s's sanitize would
 * delete, grouped by status, each checked by default but a Pending pod young enough to still
 * be scheduling; who recreates a pod is said on its row. One confirm with the count deletes the
 * checked ones, then [onSwept] (the pods list reloads). The button says why it is disabled
 * when the credentials may not delete pods there.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SanitizeSheet(namespace: String?, kube: KubeRepository, onSwept: () -> Unit, onDismiss: () -> Unit) {
    val vm: SanitizeViewModel = viewModel(key = "sanitize:${namespace.orEmpty()}", factory = factory { SanitizeViewModel(kube) })
    val state by vm.preview.collectAsStateWithLifecycle()
    val sweeping by vm.sweeping.collectAsStateWithLifecycle()
    val outcome by vm.outcome.collectAsStateWithLifecycle()
    LaunchedEffect(namespace) {
        vm.clearOutcome()
        vm.load(namespace)
    }
    val denial = rememberKubeDenial(KubeAction.SANITIZE_PODS, namespace.orEmpty())
    val pods = (state as? UiState.Loaded)?.data?.pods.orEmpty()
    var checked by remember(pods) { mutableStateOf(pods.defaultChecked()) }
    var confirming by rememberSaveable { mutableStateOf(false) }
    val context = LocalContext.current
    val count = pods.count { it.key in checked }

    if (confirming) {
        ConfirmDialog(
            title = pluralStringResource(R.plurals.sanitize_confirm_title, count, count),
            text = stringResource(R.string.sanitize_confirm_text),
            confirm = stringResource(R.string.pods_delete_confirm),
            destructive = true,
            onConfirm = {
                confirming = false
                vm.sweep(namespace, pods.sweepNames(checked)) { result ->
                    val done = result.getOrNull() ?: return@sweep
                    Toast.makeText(context, context.resources.getQuantityString(R.plurals.sanitize_done, done.deleted, done.deleted), Toast.LENGTH_SHORT).show()
                    onSwept()
                    if (done.failed.isEmpty()) onDismiss() else vm.load(namespace)
                }
            },
            onDismiss = { confirming = false },
        )
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item { SanitizeHeader(namespace) }
            outcome?.let { item { SweepOutcome(it) } }
            when (val s = state) {
                UiState.Loading -> item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                is UiState.Failed -> item { InlineError(s.message.asString()) }
                is UiState.Loaded -> {
                    if (s.data.truncated) item { InfoNotice(stringResource(R.string.sanitize_truncated)) }
                    if (pods.isEmpty()) item { EmptyText(stringResource(R.string.sanitize_empty)) }
                    pods.groupedByStatus().forEach { group ->
                        item(key = "group:${group.status}") {
                            GroupHeader(group, checked) { on -> checked = if (on) checked + group.keys else checked - group.keys }
                        }
                        items(group.pods, key = { it.key }) { pod ->
                            SanitizeRow(pod, showNamespace = namespace == null, checked = pod.key in checked) { on ->
                                checked = if (on) checked + pod.key else checked - pod.key
                            }
                        }
                    }
                }
            }
            if (pods.isNotEmpty()) item { DeleteBar(count, denial == null && !sweeping, sweeping, denial) { confirming = true } }
        }
    }
}

private val SanitizeGroup.keys: Set<String> get() = pods.map { it.key }.toSet()

@Composable
private fun SanitizeHeader(namespace: String?) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(stringResource(R.string.sanitize_title), style = MaterialTheme.typography.titleLarge, modifier = Modifier.semantics { heading() })
        MutedText(namespace?.let { stringResource(R.string.sanitize_subtitle_namespace, it) } ?: stringResource(R.string.sanitize_subtitle_all))
    }
}

@Composable
private fun SweepOutcome(outcome: Result<KubeSanitizeResult>) {
    val failure = outcome.exceptionOrNull()
    if (failure != null) {
        InlineError(failure.uiText().asString())
        return
    }
    outcome.getOrNull()?.failed?.forEach { f ->
        InlineError(stringResource(R.string.pods_delete_failed, f.name, f.error))
    }
}

@Composable
private fun GroupHeader(group: SanitizeGroup, checked: Set<String>, onToggle: (Boolean) -> Unit) {
    val inGroup = group.pods.count { it.key in checked }
    val state = when (inGroup) {
        0 -> ToggleableState.Off
        group.pods.size -> ToggleableState.On
        else -> ToggleableState.Indeterminate
    }
    Row(
        Modifier.fillMaxWidth()
            .triStateToggleable(state, role = Role.Checkbox, onClick = { onToggle(state != ToggleableState.On) })
            .padding(top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        TriStateCheckbox(state = state, onClick = null)
        Text(
            stringResource(R.string.sanitize_group, group.status, group.pods.size),
            style = MaterialTheme.typography.titleSmall,
            modifier = Modifier.padding(start = 8.dp).semantics { heading() },
        )
    }
}

@Composable
private fun SanitizeRow(pod: SanitizePod, showNamespace: Boolean, checked: Boolean, onToggle: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().toggleable(checked, role = Role.Checkbox, onValueChange = onToggle).padding(start = 16.dp, top = 2.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = null)
        Column(Modifier.padding(start = 8.dp)) {
            Text(
                if (showNamespace) pod.key else pod.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            MutedText(stringResource(R.string.sanitize_age, localizedDuration(pod.ageSec)), maxLines = 1)
            if (pod.owned) MutedText(stringResource(R.string.sanitize_recreated_by, pod.ownerKind, pod.ownerName), maxLines = 1)
            if (!pod.checkedByDefault) {
                Text(stringResource(R.string.sanitize_fresh_pending), style = MaterialTheme.typography.labelSmall, color = LocalStatusColors.current.warn)
            }
        }
    }
}

@Composable
private fun DeleteBar(count: Int, enabled: Boolean, sweeping: Boolean, denial: KubePermission?, onDelete: () -> Unit) {
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Button(
            onClick = onDelete,
            enabled = enabled && count > 0,
            colors = ButtonDefaults.buttonColors(containerColor = LocalStatusColors.current.bad),
            modifier = Modifier.fillMaxWidth(),
        ) {
            if (sweeping) CircularProgressIndicator(Modifier.size(16.dp).padding(end = 4.dp), strokeWidth = 2.dp)
            Text(pluralStringResource(R.plurals.sanitize_delete, count, count))
        }
        KubeDenialNote(denial)
    }
}
