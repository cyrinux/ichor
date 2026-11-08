package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.selection.triStateToggleable
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
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
import name.levis.ichor.data.PodSweepManager
import name.levis.ichor.data.PodSweepRun
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubeSanitizePreview
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
import name.levis.ichor.ui.operations.podSweepTally
import name.levis.ichor.ui.operations.resolve
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** The preview of a k9s-style sanitize, for one namespace (null: every namespace). */
class SanitizeViewModel(private val kube: KubeRepository) : ViewModel() {
    private val _preview = MutableStateFlow<UiState<KubeSanitizePreview>>(UiState.Loading)
    val preview: StateFlow<UiState<KubeSanitizePreview>> = _preview.asStateFlow()

    private var loading: Job? = null

    fun load(namespace: String?) {
        loading?.cancel()
        _preview.value = UiState.Loading
        loading = viewModelScope.launch {
            val read = cancellableCatching { kube.sanitizePreview(namespace.orEmpty()) }
            _preview.value = read.fold({ UiState.Loaded(it) }, { UiState.Failed(it.uiText()) })
        }
    }
}

/**
 * "Clean up dead pods" for [namespace] (null: every namespace): the pods k9s's sanitize would
 * delete, grouped by status, each checked by default but a Pending pod young enough to still
 * be scheduling; who recreates a pod is said on its row. One confirm with the count starts
 * deleting the checked ones in the background through [sweeps] (followed by the operations
 * notification) and closes the sheet. Opened while that sweep runs in [cluster], it shows how
 * far it is, with Stop; once it ended, how it went and the pods it could not delete. The
 * button says why it is disabled when the credentials may not delete pods there.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SanitizeSheet(namespace: String?, kube: KubeRepository, sweeps: PodSweepManager, cluster: String, onDismiss: () -> Unit) {
    val vm: SanitizeViewModel = viewModel(key = "sanitize:${namespace.orEmpty()}", factory = factory { SanitizeViewModel(kube) })
    val state by vm.preview.collectAsStateWithLifecycle()
    val current by sweeps.current.collectAsStateWithLifecycle()
    val sweep = current?.takeIf { it.cluster == cluster && it.namespace == namespace.orEmpty() }
    val sweeping = sweep?.running == true
    // A new preview once the sweep this sheet shows ended: what is still dead.
    LaunchedEffect(namespace, sweeping) { if (!sweeping) vm.load(namespace) }
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
                val started = runCatching { sweeps.start(cluster, namespace.orEmpty(), pods.sweepNames(checked)) }
                val notice = started.fold(
                    { if (it) context.getString(R.string.pod_sweep_started) else context.getString(R.string.pod_sweep_busy) },
                    { it.uiText().resolve(context) },
                )
                Toast.makeText(context, notice, Toast.LENGTH_SHORT).show()
                if (started.getOrNull() == true) onDismiss()
            },
            onDismiss = { confirming = false },
        )
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item { SanitizeHeader(namespace) }
            if (sweep != null && sweep.running) {
                item { SweepProgress(sweep, onStop = sweeps::stop) }
                return@LazyColumn
            }
            sweep?.let { item { SweepOutcome(it) } }
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
            if (pods.isNotEmpty()) item { DeleteBar(count, denial == null, denial) { confirming = true } }
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

/** The running sweep: how many pods were tried, a bar, and Stop. */
@Composable
private fun SweepProgress(sweep: PodSweepRun, onStop: () -> Unit) {
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            if (sweep.stopping) stringResource(R.string.pod_sweep_stopping) else stringResource(R.string.pod_sweep_progress, sweep.done.toString(), sweep.total.toString()),
            style = MaterialTheme.typography.bodyMedium,
        )
        LinearProgressIndicator(progress = { sweep.fraction }, modifier = Modifier.fillMaxWidth())
        MutedText(stringResource(R.string.pod_sweep_background))
        IchorOutlinedButton(onClick = onStop, enabled = !sweep.stopping, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.operation_stop)) }
    }
}

/** How the last sweep went, and each pod it could not delete. */
@Composable
private fun SweepOutcome(sweep: PodSweepRun) {
    val context = LocalContext.current
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        sweep.error?.let { InlineError(it) }
        if (sweep.error == null) {
            val title = if (sweep.stopped) R.string.pod_sweep_stopped else R.string.pod_sweep_done
            Text(stringResource(title, sweep.namespace.ifEmpty { stringResource(R.string.pod_sweep_every_namespace) }), style = MaterialTheme.typography.titleSmall)
            MutedText(podSweepTally(sweep).resolve(context))
        }
        sweep.failures.forEach { f -> InlineError(stringResource(R.string.pods_delete_failed, f.name, f.error)) }
    }
}

/** A running sweep above the pods list: where, how far, a bar. */
@Composable
internal fun PodSweepBanner(sweep: PodSweepRun, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(
            stringResource(R.string.pod_sweep_notification_title, sweep.namespace.ifEmpty { stringResource(R.string.pod_sweep_every_namespace) }),
            style = MaterialTheme.typography.labelLarge,
        )
        MutedText(stringResource(R.string.pod_sweep_progress, sweep.done.toString(), sweep.total.toString()))
        LinearProgressIndicator(progress = { sweep.fraction }, modifier = Modifier.fillMaxWidth())
    }
}

private val PodSweepRun.fraction: Float get() = if (total > 0) done.toFloat() / total else 0f

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
private fun DeleteBar(count: Int, enabled: Boolean, denial: KubePermission?, onDelete: () -> Unit) {
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        IchorButton(
            onClick = onDelete,
            enabled = enabled && count > 0,
            colors = ButtonDefaults.buttonColors(containerColor = LocalStatusColors.current.bad),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(pluralStringResource(R.plurals.sanitize_delete, count, count))
        }
        KubeDenialNote(denial)
    }
}
