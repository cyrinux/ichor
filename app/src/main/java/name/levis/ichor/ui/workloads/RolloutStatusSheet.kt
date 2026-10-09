package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import java.text.NumberFormat
import name.levis.ichor.R
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.watchForever
import name.levis.ichor.model.KubeRolloutPod
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

/** How long the sheet waits before following again a watch that ended (the network). */
private const val WATCH_RETRY_MILLIS = 2_000L

/**
 * The live rollout of the workload [restarts] follows after a restart, like
 * `kubectl rollout status`: the Go core reads it again each time the workload or one of its
 * pods changes, until every pod runs the new template and is ready. Closing it only stops
 * following; the rollout goes on in the cluster.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RolloutStatusSheet(restarts: WorkloadRestarts) {
    val following by restarts.following.collectAsStateWithLifecycle()
    val workload = following ?: return
    var status by remember(workload.key) { mutableStateOf<KubeRolloutStatus?>(null) }
    var error by remember(workload.key) { mutableStateOf<UiText?>(null) }
    var ended by remember(workload.key) { mutableStateOf(false) }
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    // Followed while the sheet is visible; a watch that ends is followed again after a moment.
    LaunchedEffect(workload.key, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            watchForever(WATCH_RETRY_MILLIS, start = { restarts.rolloutWatch(workload) }) { item ->
                when (item) {
                    is StreamItem.Item -> {
                        status = item.value
                        error = null
                        if (item.value.done && !ended) {
                            ended = true
                            restarts.rolloutEnded()
                        }
                    }
                    is StreamItem.Done -> item.error?.let { error = UiText.Raw(it) }
                }
            }
        }
    }

    ModalBottomSheet(onDismissRequest = restarts::stopFollowing, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            item { Header(workload, status) }
            item { Progress(status) }
            error?.let { item { InlineError(it.asString()) } }
            val pods = status?.pods
            if (pods != null) {
                if (pods.isEmpty()) {
                    item { Text(stringResource(R.string.rollout_no_pods), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                } else {
                    item { HorizontalDivider() }
                    items(pods, key = { it.name }) { PodRow(it) }
                }
            }
            item {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (status?.done != true) InfoNotice(stringResource(R.string.rollout_leave_hint), Modifier.weight(1f)) else Spacer(Modifier.weight(1f))
                    TextButton(onClick = restarts::stopFollowing) { Text(stringResource(R.string.argo_net_close)) }
                }
            }
        }
    }
}

@Composable
private fun Header(workload: KubeWorkload, status: KubeRolloutStatus?) {
    val colors = LocalStatusColors.current
    val (label, color) = when {
        status == null -> stringResource(R.string.rollout_starting) to colors.muted
        status.manual -> stringResource(R.string.rollout_manual) to colors.warn
        status.done -> stringResource(R.string.rollout_done) to colors.ok
        status.failed -> stringResource(R.string.rollout_failed) to colors.bad
        else -> stringResource(R.string.workloads_state_progressing) to colors.warn
    }
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(stringResource(R.string.workloads_restart_done, workload.name), style = MaterialTheme.typography.titleLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
        Text("${workload.kind}  ·  ${workload.namespace}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(label, style = MaterialTheme.typography.labelLarge, color = color, modifier = Modifier.padding(top = 4.dp))
    }
}

/** New pods ready out of desired as a segmented bar, with the percentage and the controller's counters. */
@Composable
private fun Progress(status: KubeRolloutStatus?) {
    val w = status?.workload
    if (status == null || w == null) {
        LinearProgressIndicator(Modifier.fillMaxWidth())
        return
    }
    val newReady = status.pods.count { it.updated && it.healthy }
    val newStarting = status.pods.count { it.updated && !it.healthy }
    val fraction = if (status.done) 1f else newReady.coerceAtMost(w.desired).toFloat() / w.desired.coerceAtLeast(1)
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        RolloutProgressBar(w.desired, newReady, newStarting, done = status.done && !status.manual, failed = status.failed)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                stringResource(R.string.rollout_counts, w.updated, w.desired, w.ready, w.available),
                style = MaterialTheme.typography.labelSmall,
                color = muted,
                modifier = Modifier.weight(1f),
            )
            if (!status.manual) {
                Text(NumberFormat.getPercentInstance().format(fraction), style = MaterialTheme.typography.labelLarge, fontFamily = FontFamily.Monospace)
            }
        }
    }
}

@Composable
private fun PodRow(pod: KubeRolloutPod) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Column(Modifier.weight(1f)) {
            Text(pod.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val details = listOfNotNull(
                stringResource(R.string.pods_ready_count, pod.ready, pod.containers),
                pluralStringResource(R.plurals.pods_restarts, pod.restarts, pod.restarts).takeIf { pod.restarts > 0 },
                pod.node.takeIf { it.isNotEmpty() },
            )
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(pod.status, style = MaterialTheme.typography.labelSmall, color = podColor(pod))
                Text(details.joinToString("  ·  "), style = MaterialTheme.typography.labelSmall, color = muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        StatusPill(
            stringResource(if (pod.updated) R.string.rollout_pod_new else R.string.rollout_pod_old),
            if (pod.updated) colors.ok else colors.muted,
        )
    }
}

@Composable
private fun podColor(pod: KubeRolloutPod): Color {
    val colors = LocalStatusColors.current
    val failing = listOf("Error", "BackOff", "Failed", "OOM", "Err").any { pod.status.contains(it) }
    return when {
        pod.healthy -> colors.ok
        failing -> colors.bad
        pod.status == "Terminating" -> colors.muted
        else -> colors.warn
    }
}
