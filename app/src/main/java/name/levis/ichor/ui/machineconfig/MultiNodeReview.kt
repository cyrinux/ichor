package name.levis.ichor.ui.machineconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.RemoveCircleOutline
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.MultiApplyRun
import name.levis.ichor.model.MultiConfigNodePreview
import name.levis.ichor.model.MultiConfigNodeState
import name.levis.ichor.model.MultiConfigPreview
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

/** The nodes to give the change too, control planes then workers; this node is picked already. */
@Composable
fun MultiNodePickerDialog(
    candidates: UiState<List<NodeOverview>>,
    selected: Set<String>,
    onToggle: (String) -> Unit,
    onRetry: () -> Unit,
    onPreview: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.machine_config_multi_pick_title)) },
        text = {
            when (candidates) {
                UiState.Loading -> CircularProgressIndicator()
                is UiState.Failed -> ErrorBox(candidates.message, onRetry)
                is UiState.Loaded -> LazyColumn(Modifier.heightIn(max = 420.dp)) {
                    val (planes, workers) = candidates.data.partition { it.role == "controlplane" }
                    listOf(R.string.machine_config_multi_control_planes to planes, R.string.machine_config_multi_workers to workers)
                        .filter { it.second.isNotEmpty() }
                        .forEach { (title, nodes) ->
                            item(key = "title-$title") {
                                Text(stringResource(title), style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(top = 8.dp))
                            }
                            items(nodes, key = { it.node }) { node -> PickRow(node, node.node in selected) { onToggle(node.node) } }
                        }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onPreview, enabled = candidates is UiState.Loaded && selected.isNotEmpty()) {
                Text(stringResource(R.string.machine_config_multi_preview))
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun PickRow(node: NodeOverview, checked: Boolean, onToggle: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().toggleable(checked, role = Role.Checkbox, onValueChange = { onToggle() }).padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = null)
        Column(Modifier.padding(start = 8.dp)) {
            Text(node.hostname.ifEmpty { node.node }, maxLines = 1, overflow = TextOverflow.Ellipsis)
            MutedText(node.node)
        }
    }
}

/**
 * Each node's own diff, a page per node, then the ways to apply the change to the nodes it
 * would change ([onApply]); nodes it does not fit say why and are skipped.
 */
@Composable
fun MultiNodeReviewContent(
    preview: UiState<MultiConfigPreview>,
    onRetry: () -> Unit,
    onApply: (ConfigApplyMode) -> Unit,
    modifier: Modifier = Modifier,
) {
    when (preview) {
        UiState.Loading -> Waiting(stringResource(R.string.machine_config_review_loading), modifier)
        is UiState.Failed -> ErrorBox(preview.message, onRetry, modifier)
        is UiState.Loaded -> {
            val data = preview.data
            val pager = rememberPagerState { data.nodes.size }
            val scope = rememberCoroutineScope()
            Column(modifier.fillMaxSize()) {
                PrimaryScrollableTabRow(selectedTabIndex = pager.currentPage, edgePadding = 8.dp) {
                    data.nodes.forEachIndexed { i, node ->
                        Tab(selected = pager.currentPage == i, onClick = { scope.launch { pager.animateScrollToPage(i) } }, text = { Text(node.name) })
                    }
                }
                HorizontalPager(pager, Modifier.weight(1f)) { page -> NodePage(data.nodes[page]) }
                Column(
                    Modifier.verticalScroll(rememberScrollState()).padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    val changing = data.changing.size
                    if (changing == 0) {
                        InfoBox(stringResource(R.string.machine_config_multi_nothing))
                    } else {
                        MutedText(stringResource(R.string.machine_config_multi_summary, changing, data.nodes.size))
                        ApplyChoice(data.applyModes, onApply)
                    }
                }
            }
        }
    }
}

@Composable
private fun NodePage(node: MultiConfigNodePreview) {
    val status = LocalStatusColors.current
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            when {
                node.error != null -> StatusPill(stringResource(R.string.machine_config_multi_skipped), status.bad)
                !node.changed -> StatusPill(stringResource(R.string.machine_config_multi_unchanged), status.muted)
                node.needsReboot -> StatusPill(stringResource(R.string.machine_config_multi_reboot), status.warn)
            }
        }
        when {
            node.error != null -> InfoBox(stringResource(R.string.machine_config_multi_does_not_fit, node.error))
            !node.changed -> InfoBox(stringResource(R.string.machine_config_no_changes))
            else -> DiffView(node.lines, Modifier.weight(1f))
        }
    }
}

/** A multi-node apply: every node's state as it goes, then how it ended. */
@Composable
fun MultiApplyContent(run: MultiApplyRun, onDone: () -> Unit, modifier: Modifier = Modifier) {
    val status = LocalStatusColors.current
    val progress = run.progress
    Column(modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (progress != null && progress.total > 0) {
            Text(
                stringResource(R.string.machine_config_multi_progress, (progress.index + 1).coerceAtMost(progress.total), progress.total),
                style = MaterialTheme.typography.titleMedium,
            )
        }
        LazyColumn(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(progress?.nodes.orEmpty(), key = { it.node }) { node ->
                val running = !run.finished && node.state == MultiConfigNodeState.APPLYING
                NodeStateRow(node, if (running) progress?.message.orEmpty() else node.error.orEmpty())
            }
        }
        if (run.finished) {
            val failed = run.error != null
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Icon(
                    if (failed) Icons.Outlined.ErrorOutline else Icons.Outlined.CheckCircle,
                    contentDescription = null,
                    tint = if (failed) status.bad else status.ok,
                )
                Text(
                    when {
                        !failed -> stringResource(R.string.machine_config_multi_done)
                        run.error.isNullOrEmpty() -> stringResource(R.string.machine_config_try_no_answer)
                        else -> run.error
                    },
                )
            }
            Button(onClick = onDone, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(if (failed) R.string.machine_config_back_to_draft else R.string.machine_config_done))
            }
        }
    }
}

@Composable
private fun NodeStateRow(node: MultiConfigNodeState, detail: String) {
    val status = LocalStatusColors.current
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        when (node.state) {
            MultiConfigNodeState.APPLYING -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            else -> {
                val (icon, tint) = when (node.state) {
                    MultiConfigNodeState.DONE -> Icons.Outlined.CheckCircle to status.ok
                    MultiConfigNodeState.FAILED -> Icons.Outlined.ErrorOutline to status.bad
                    MultiConfigNodeState.SKIPPED -> Icons.Outlined.Block to status.warn
                    MultiConfigNodeState.UNCHANGED -> Icons.Outlined.RemoveCircleOutline to status.muted
                    else -> Icons.Outlined.RadioButtonUnchecked to status.muted
                }
                Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(20.dp))
            }
        }
        Column(Modifier.weight(1f)) {
            Text(node.name)
            val state = stringResource(
                when (node.state) {
                    MultiConfigNodeState.APPLYING -> R.string.machine_config_multi_state_applying
                    MultiConfigNodeState.DONE -> R.string.machine_config_multi_state_done
                    MultiConfigNodeState.FAILED -> R.string.machine_config_multi_state_failed
                    MultiConfigNodeState.SKIPPED -> R.string.machine_config_multi_skipped
                    MultiConfigNodeState.UNCHANGED -> R.string.machine_config_multi_unchanged
                    else -> R.string.machine_config_multi_state_pending
                },
            )
            MutedText(listOf(state, detail).filter { it.isNotEmpty() }.joinToString(" · "))
        }
    }
}
