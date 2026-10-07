package name.levis.ichor.ui.apihealth

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PersonSearch
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ApiCount
import name.levis.ichor.model.ApiHealthReport
import name.levis.ichor.model.formatMs
import name.levis.ichor.model.formatRate
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.components.pageContent

/** Reading it scrapes /metrics twice a few seconds apart: loaded on demand, never polled. */
class ApiHealthViewModel(private val talos: TalosRepository) : LoadingViewModel<ApiHealthReport>() {
    override suspend fun fetch() = talos.apiHealth()
}

/**
 * The Kubernetes API server's health and what puts pressure on it (os:admin): its readyz and
 * livez checks, the request load over the last seconds, which clients (flow schemas) send it,
 * how full each priority level is, the busiest requests, what waits in the queues and who
 * sent it, open watches and the largest object counts.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ApiHealthScreen(
    onBack: () -> Unit,
    onAudit: () -> Unit,
    vm: ApiHealthViewModel = viewModel(factory = factory { ApiHealthViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.apihealth_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        Loaded(state, vm::refresh, modifier) { data ->
            ApiHealthList(data, onAudit)
            
        }
    }
}

@Composable
private fun ApiHealthList(report: ApiHealthReport, onAudit: () -> Unit) {
    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "verdict") { VerdictHeader(report) }
        // The metrics group clients; the audit log names them, and what they do wrong.
        item(key = "audit") {
            OutlinedButton(onClick = onAudit, modifier = Modifier.padding(horizontal = 16.dp)) {
                Icon(Icons.Outlined.PersonSearch, contentDescription = null)
                Text(stringResource(R.string.audit_open), Modifier.padding(start = 8.dp))
            }
        }
        if (report.metricsError.isNotEmpty()) {
            item(key = "metrics-error") {
                InlineError(stringResource(R.string.apihealth_metrics_error, report.metricsError), Modifier.padding(horizontal = 16.dp))
            }
            return@LazyColumn
        }
        item(key = "load") { LoadSection(report) }
        clientsSection(report)
        prioritiesSection(report)
        requestsSection(report)
        queuedSection(report)
        countsSection("watched", R.string.apihealth_watched, report.watchedKinds)
        countsSection("objects", R.string.apihealth_objects, report.objects)
    }
}

/** The request load, inflight work and queues, with what is wrong in the warning colours. */
@Composable
private fun LoadSection(r: ApiHealthReport) {
    val colors = LocalStatusColors.current
    Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
        SectionTitle(stringResource(R.string.apihealth_load))
        InfoRow(stringResource(R.string.apihealth_requests), formatRate(r.requestRate))
        InfoRow(stringResource(R.string.apihealth_errors), formatRate(r.errorRate), valueColor = if (r.errorRate > 0) colors.warn else colors.muted)
        InfoRow(stringResource(R.string.apihealth_throttled), formatRate(r.throttledRate), valueColor = if (r.throttledRate > 0) colors.bad else colors.muted)
        InfoRow(stringResource(R.string.apihealth_rejected), formatRate(r.rejectedRate), valueColor = if (r.rejectedRate > 0) colors.bad else colors.muted)
        InfoRow(stringResource(R.string.apihealth_inflight), stringResource(R.string.apihealth_inflight_value, r.inflightRead, r.inflightMutate))
        InfoRow(stringResource(R.string.apihealth_queued), r.queued.toString(), valueColor = if (r.queued > 0) colors.warn else colors.muted)
        InfoRow(stringResource(R.string.apihealth_watches), r.watches.toString())
        InfoRow(stringResource(R.string.apihealth_watch_events), formatRate(r.watchEventRate))
        if (r.etcdLatencyMs > 0) InfoRow(stringResource(R.string.apihealth_etcd), formatMs(r.etcdLatencyMs))
    }
}

private fun LazyListScope.clientsSection(r: ApiHealthReport) {
    if (r.clients.isEmpty()) return
    item(key = "clients-title") {
        Column(Modifier.padding(horizontal = 16.dp)) {
            SectionTitle(stringResource(R.string.apihealth_clients))
            MutedText(stringResource(R.string.apihealth_clients_hint))
        }
    }
    // Sorted rejections first: the bars scale on the busiest, wherever it is listed. A flow
    // schema moved to another level keeps its old counters: name and level make the key.
    val top = r.clients.maxOf { it.rate }
    items(r.clients, key = { "client-${it.name}/${it.priority}" }) { ClientRow(it, top = top) }
}

private fun LazyListScope.prioritiesSection(r: ApiHealthReport) {
    if (r.priorities.isEmpty()) return
    item(key = "priorities-title") { SectionTitle(stringResource(R.string.apihealth_priorities), Modifier.padding(horizontal = 16.dp)) }
    items(r.priorities, key = { "priority-${it.name}" }) { PriorityRow(it) }
}

private fun LazyListScope.requestsSection(r: ApiHealthReport) {
    item(key = "requests-title") { SectionTitle(stringResource(R.string.apihealth_requests_top), Modifier.padding(horizontal = 16.dp)) }
    if (r.requests.isEmpty()) {
        item(key = "requests-empty") { MutedText(stringResource(R.string.apihealth_no_traffic), Modifier.padding(horizontal = 16.dp)) }
    }
    items(r.requests, key = { "request-${it.verb}-${it.resource}" }) { RequestRow(it) }
}

private fun LazyListScope.queuedSection(r: ApiHealthReport) {
    if (r.queuedRequests.isEmpty()) return
    item(key = "queued-title") { SectionTitle(stringResource(R.string.apihealth_waiting), Modifier.padding(horizontal = 16.dp)) }
    items(r.queuedRequests.withIndex().toList(), key = { "queued-${it.index}" }) { QueuedRow(it.value) }
}

private fun LazyListScope.countsSection(key: String, @StringRes title: Int, rows: List<ApiCount>) {
    if (rows.isEmpty()) return
    item(key = "$key-title") {
        Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            HorizontalDivider(Modifier.padding(top = 8.dp))
            SectionTitle(stringResource(title))
            rows.forEach { CountRow(it) }
        }
    }
}
