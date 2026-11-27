package name.levis.ichor.wear

import android.os.Bundle
import android.text.format.DateUtils
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.wear.compose.foundation.lazy.ScalingLazyColumn
import androidx.wear.compose.foundation.lazy.ScalingLazyListScope
import androidx.wear.compose.foundation.lazy.rememberScalingLazyListState
import androidx.wear.compose.material3.AppScaffold
import androidx.wear.compose.material3.Button
import androidx.wear.compose.material3.ListHeader
import androidx.wear.compose.material3.MaterialTheme
import androidx.wear.compose.material3.ScreenScaffold
import androidx.wear.compose.material3.Text
import androidx.wear.compose.navigation.SwipeDismissableNavHost
import androidx.wear.compose.navigation.composable
import androidx.wear.compose.navigation.rememberSwipeDismissableNavController
import kotlinx.coroutines.delay
import name.levis.ichor.widget.Tone
import name.levis.ichor.widget.WIDGET_STALE_AFTER_MS
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.WearNode
import name.levis.ichor.widget.WearNodeHealth
import name.levis.ichor.widget.WearNodeProblem
import name.levis.ichor.widget.WearPayload
import name.levis.ichor.widget.toneOf

private const val LIST = "clusters"
private const val CLUSTER = "cluster"
private const val MINUTE_MS = 60_000L

/**
 * The watch app: the clusters of the phone's last check, then a cluster's issues and nodes.
 * Read-only, from the payload the phone sends (WatchSnapshotStore): no credential on the watch.
 * The cluster tile opens it on its cluster ([EXTRA_CLUSTER]), the fleet tile on the list.
 */
class WatchActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val start = intent.getIntExtra(EXTRA_CLUSTER, -1).takeIf { savedInstanceState == null }
        setContent { MaterialTheme { WatchApp(start) } }
    }
}

@Composable
private fun WatchApp(start: Int?) {
    val context = LocalContext.current
    var payload by remember { mutableStateOf(WatchSnapshotStore.load(context)) }
    DisposableEffect(context) {
        val stop = WatchSnapshotStore.observe(context) { payload = WatchSnapshotStore.load(context) }
        WatchSnapshotStore.fetchIfMissing(context) { payload = WatchSnapshotStore.load(context) }
        onDispose(stop)
    }
    // Moves the ages on and turns a check stale without a new payload.
    val now by produceState(System.currentTimeMillis()) {
        while (true) {
            delay(MINUTE_MS)
            value = System.currentTimeMillis()
        }
    }
    val nav = rememberSwipeDismissableNavController()
    LaunchedEffect(Unit) {
        if (start != null && start >= 0) nav.navigate("$CLUSTER/$start")
    }
    AppScaffold {
        SwipeDismissableNavHost(navController = nav, startDestination = LIST) {
            composable(LIST) { ClusterList(payload, now) { nav.navigate("$CLUSTER/$it") } }
            composable("$CLUSTER/{index}") { entry ->
                val index = entry.arguments?.getString("index")?.toIntOrNull() ?: -1
                val cluster = clusterAt(payload?.clusters.orEmpty(), index)
                if (cluster == null) ClusterList(payload, now) { nav.navigate("$CLUSTER/$it") } else ClusterPage(cluster, now)
            }
        }
    }
}

@Composable
private fun Page(content: ScalingLazyListScope.() -> Unit) {
    val state = rememberScalingLazyListState()
    ScreenScaffold(scrollState = state) { padding ->
        ScalingLazyColumn(
            state = state,
            contentPadding = padding,
            modifier = Modifier.fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(4.dp),
            content = content,
        )
    }
}

@Composable
private fun ClusterList(payload: WearPayload?, now: Long, open: (Int) -> Unit) {
    val clusters = payload?.clusters.orEmpty()
    Page {
        item { ListHeader { Text(stringResource(R.string.wear_clusters_title)) } }
        if (clusters.isEmpty()) {
            item { Note(stringResource(R.string.wear_no_data)) }
            return@Page
        }
        clusters.forEachIndexed { index, cluster ->
            item { ClusterButton(cluster, now) { open(index) } }
        }
        item { LastCheck(lastCheck(clusters), now) }
    }
}

@Composable
private fun ClusterButton(cluster: WearCluster, now: Long, onClick: () -> Unit) {
    val row = clusterRow(cluster, now)
    val res = LocalContext.current.resources
    val detail = when {
        row.takenAt <= 0 -> stringResource(R.string.wear_no_check)
        row.stale -> staleText(res, row.takenAt, now)
        row.problem != null -> "${statusText(res, row.problem)} · ${shortAge(now - row.takenAt)}"
        else -> "${stringResource(R.string.wear_tone_ok)} · ${shortAge(now - row.takenAt)}"
    }
    Button(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        icon = { Dot(if (row.stale) Color(WatchColors.DIMMED) else Color(WatchColors.of(row.tone))) },
        secondaryLabel = { Text(detail, maxLines = 2, overflow = TextOverflow.Ellipsis) },
    ) {
        Text(row.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun ClusterPage(cluster: WearCluster, now: Long) {
    val res = LocalContext.current.resources
    val row = clusterRow(cluster, now)
    val issues = clusterIssues(cluster)
    Page {
        item { ListHeader { Text(cluster.label, maxLines = 2, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center) } }
        item {
            val color = if (row.stale) WatchColors.DIMMED else WatchColors.of(row.tone)
            Text(
                stringResource(R.string.wear_ready_count, cluster.counts.ready, cluster.counts.total),
                color = Color(color),
                style = MaterialTheme.typography.titleMedium,
            )
        }
        if (row.stale || row.takenAt <= 0) {
            item { LastCheck(row.takenAt.takeIf { it > 0 }, now) }
        } else {
            item { ListHeader { Text(stringResource(R.string.wear_issues_header)) } }
            if (issues.isEmpty()) {
                item { Line(Tone.OK, stringResource(R.string.wear_no_issues)) }
            } else {
                issues.forEach { issue ->
                    item { Line(toneOf(issue), statusText(res, issue)) }
                }
            }
        }
        if (cluster.nodes.isNotEmpty()) {
            item { ListHeader { Text(stringResource(R.string.wear_nodes_header)) } }
            cluster.nodes.forEachIndexed { index, node -> item { NodeLine(node, index) } }
        } else if (cluster.counts.total > 0) {
            item { Note(stringResource(R.string.wear_no_node_list)) }
        }
    }
}

@Composable
private fun NodeLine(node: WearNode, index: Int) {
    val name = node.name.ifEmpty { stringResource(R.string.wear_node_number, index + 1) }
    val state = when (node.health) {
        WearNodeHealth.READY -> stringResource(R.string.wear_node_ready)
        WearNodeHealth.NOT_READY -> stringResource(R.string.wear_node_not_ready)
        WearNodeHealth.UNREACHABLE -> node.problem?.let { stringResource(problemText(it)) } ?: stringResource(R.string.wear_node_unreachable)
    }
    Line(nodeTone(node.health), name, state)
}

private fun problemText(p: WearNodeProblem): Int = when (p) {
    WearNodeProblem.TIMEOUT -> R.string.wear_problem_timeout
    WearNodeProblem.REFUSED -> R.string.wear_problem_refused
    WearNodeProblem.NO_SUCH_HOST -> R.string.wear_problem_no_such_host
    WearNodeProblem.TLS -> R.string.wear_problem_tls
    WearNodeProblem.UNREACHABLE -> R.string.wear_problem_unreachable
}

/**
 * A dot in [tone]'s colour, a label and an optional second line in that colour too (as the
 * Apple Watch app's node state), read as one by a screen reader.
 */
@Composable
private fun Line(tone: Tone, title: String, detail: String? = null) {
    val color = Color(WatchColors.of(tone))
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 2.dp).semantics(mergeDescendants = true) {},
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Dot(color)
        Column(modifier = Modifier.padding(start = 8.dp)) {
            Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium)
            if (detail != null) {
                Text(detail, color = color, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@Composable
private fun Dot(color: Color) {
    Box(modifier = Modifier.size(8.dp).background(color, CircleShape))
}

@Composable
private fun Note(text: String) {
    Text(
        text,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp),
        textAlign = TextAlign.Center,
        style = MaterialTheme.typography.bodySmall,
        color = Color(WatchColors.SECONDARY),
    )
}

/** "Last check 5 minutes ago", and a hint to open the phone once it is stale. */
@Composable
private fun LastCheck(takenAt: Long?, now: Long) {
    if (takenAt == null) return Note(stringResource(R.string.wear_no_data))
    val age = DateUtils.getRelativeTimeSpanString(takenAt, now, DateUtils.MINUTE_IN_MILLIS).toString()
    Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.fillMaxWidth()) {
        Note(stringResource(R.string.wear_last_check, age))
        if (now - takenAt > WIDGET_STALE_AFTER_MS) Note(stringResource(R.string.wear_refresh_on_phone))
    }
}
