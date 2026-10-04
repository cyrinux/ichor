package name.levis.ichor.ui.support

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.SupportBundleFile
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.BUNDLE_CLUSTER
import name.levis.ichor.model.BundleProgress
import name.levis.ichor.model.BundleRowState
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.allows
import name.levis.ichor.model.notice
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.RoleNotice
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.rememberClusterSupport
import name.levis.ichor.ui.components.rememberSaveFile
import name.levis.ichor.ui.components.shareFile
import name.levis.ichor.ui.components.text
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import java.io.File
import name.levis.ichor.util.formatDateTime

const val SUPPORT_BUNDLE_MIME = "application/zip"

/** `talosctl support`: collect logs and cluster details of the chosen nodes into a zip. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SupportBundleScreen(
    onBack: () -> Unit,
    vm: SupportBundleViewModel = viewModel(
        factory = factory { SupportBundleViewModel(app.supportBundleRepository, app.talosRepository, app.configRepository) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val config by vm.configs.config.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var deleting by remember { mutableStateOf<File?>(null) }
    val save = rememberSaveFile(SUPPORT_BUNDLE_MIME, R.string.support_bundle_saved, R.string.support_bundle_save_failed) { message ->
        scope.launch { snackbar.showSnackbar(message.resolve(context)) }
    }
    val support = rememberClusterSupport(TalosFeature.SUPPORT_BUNDLE, state.nodes?.filter { it.reachable }?.map { it.node })
    val summary = config?.activeSummary
    LaunchedEffect(Unit) { vm.load() }

    // Collecting takes minutes: do not let the display sleep (which would suspend the app).
    if (state.run is SupportRun.Running) KeepScreenOn()

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.support_bundle_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        LazyColumn(
            Modifier.padding(padding).fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Text(
                    stringResource(R.string.support_bundle_warning),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.warn,
                )
                MutedText(
                    stringResource(R.string.support_bundle_role_note),
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            when {
                summary != null && !summary.allows(Feature.SUPPORT_BUNDLE) -> item { RoleNotice(Feature.SUPPORT_BUNDLE, summary.roles) }
                support.notice != null -> item { support.notice?.let { InfoNotice(it.text()) } }
                else -> item { RunCard(state, vm) }
            }
            item { SectionTitle(stringResource(R.string.support_bundle_files)) }
            if (state.files.isEmpty()) {
                item { Text(stringResource(R.string.support_bundle_files_empty), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            items(state.files, key = { it.name }) { file ->
                BundleRow(
                    file,
                    onSave = { save(file.file) },
                    onShare = { shareFile(context, file.file, SUPPORT_BUNDLE_MIME, R.string.support_bundle_share_chooser) },
                    onDelete = { deleting = file.file },
                )
            }
        }
    }

    deleting?.let { file ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text(stringResource(R.string.support_bundle_delete_title)) },
            text = { Text(stringResource(R.string.capture_delete_body, file.name)) },
            confirmButton = {
                TextButton(onClick = {
                    deleting = null
                    vm.delete(file)
                }) { Text(stringResource(R.string.common_delete)) }
            },
            dismissButton = { TextButton(onClick = { deleting = null }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/** Node choice and "Create bundle", or the progress of the running collection. */
@Composable
private fun RunCard(state: SupportBundleState, vm: SupportBundleViewModel) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            when (val run = state.run) {
                is SupportRun.Running -> {
                    Text(stringResource(R.string.support_bundle_running), style = MaterialTheme.typography.titleSmall)
                    val fraction = run.progress.overallFraction(run.nodes)
                    LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth(), drawStopIndicator = {})
                    run.nodes.forEach { node -> NodeProgress(state.hostname(node), node, run.progress) }
                    // Cluster-wide steps (etcd, writing the archive) are reported without a node.
                    NodeProgress(stringResource(R.string.support_bundle_cluster), BUNDLE_CLUSTER, run.progress)
                    TextButton(onClick = vm::cancel) { Text(stringResource(R.string.common_cancel)) }
                }
                else -> {
                    (run as? SupportRun.Failed)?.let { InlineError(stringResource(R.string.support_bundle_failed, it.message.asString())) }
                    (run as? SupportRun.Done)?.let {
                        Text(stringResource(R.string.support_bundle_ready, it.name, formatBytes(it.size)), color = colors.ok)
                    }
                    SectionTitle(stringResource(R.string.support_bundle_nodes))
                    val nodes = state.nodes
                    when {
                        nodes == null && state.nodesError != null -> {
                            InlineError(state.nodesError.asString())
                            TextButton(onClick = vm::load) { Text(stringResource(R.string.common_retry)) }
                        }
                        nodes == null -> LinearProgressIndicator(Modifier.fillMaxWidth())
                        else -> nodes.forEach { node -> NodeChoice(node, node.node in state.selected) { vm.toggle(node.node) } }
                    }
                    Button(onClick = vm::start, enabled = state.selected.isNotEmpty(), modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.support_bundle_create))
                    }
                }
            }
        }
    }
}

@Composable
private fun NodeChoice(node: NodeOverview, selected: Boolean, onToggle: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().toggleable(value = selected, role = Role.Checkbox, onValueChange = { onToggle() }),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = selected, onCheckedChange = null, modifier = Modifier.padding(end = 12.dp, top = 8.dp, bottom = 8.dp))
        Column {
            Text(node.hostname, style = MaterialTheme.typography.bodyMedium)
            Text(
                if (node.reachable) node.node else "${node.node} · ${stringResource(R.string.common_status_unreachable)}",
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = if (node.reachable) MaterialTheme.colorScheme.onSurfaceVariant else LocalStatusColors.current.warn,
            )
        }
    }
}

@Composable
private fun NodeProgress(label: String, node: String, progress: BundleProgress) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row {
        Text(label, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
        when (progress.stateOf(node)) {
            BundleRowState.WAITING -> Text(stringResource(R.string.support_bundle_node_waiting), style = MaterialTheme.typography.bodySmall, color = muted)
            BundleRowState.DONE -> Text(
                stringResource(R.string.support_bundle_node_done),
                style = MaterialTheme.typography.bodySmall,
                color = LocalStatusColors.current.ok,
            )
            BundleRowState.COLLECTING -> Text(
                progress.stepOf(node).orEmpty().ifBlank { "…" },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.primary,
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun BundleRow(file: SupportBundleFile, onSave: () -> Unit, onShare: () -> Unit, onDelete: () -> Unit) {
    val date = remember(file.modified) { formatDateTime(file.modified) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp)) {
            Text(file.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            MutedText("${formatBytes(file.size)} · $date")
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                TextButton(onClick = onSave) { Text(stringResource(R.string.capture_save)) }
                TextButton(onClick = onShare) { Text(stringResource(R.string.capture_share)) }
                TextButton(onClick = onDelete) { Text(stringResource(R.string.common_delete)) }
            }
        }
    }
}
