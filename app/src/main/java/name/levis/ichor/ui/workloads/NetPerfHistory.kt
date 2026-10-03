package name.levis.ichor.ui.workloads

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.NETPERF_HISTORY_LIMIT
import name.levis.ichor.model.NETPERF_LATENCY
import name.levis.ichor.model.NETPERF_PATH_POD
import name.levis.ichor.model.NETPERF_THROUGHPUT
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.formatMbps
import name.levis.ichor.model.formatMicros
import name.levis.ichor.model.setup
import name.levis.ichor.ui.factory
import java.text.DateFormat
import java.util.Date

/**
 * The network test of the active cluster, for the screen showing it (Kubernetes or Cluster
 * insights): one per cluster and privacy mask, which its saved tests are kept under.
 */
@Composable
fun netPerfViewModel(): NetPerfViewModel {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val scope = "${config?.activeSummary?.fingerprint.orEmpty()}-${mask.storageKey}"
    return viewModel(key = "netperf-$scope", factory = factory { NetPerfViewModel(app.netPerfRepository, app.netPerfHistory, scope) })
}

/** The saved tests under the setup, newest first; one opens its results. */
@Composable
internal fun NetPerfHistoryList(history: List<NetPerfReport>, onOpen: (NetPerfReport) -> Unit) {
    if (history.isEmpty()) return
    val gutter = Modifier.padding(horizontal = 16.dp)
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(gutter.padding(top = 12.dp)) {
        Text(stringResource(R.string.netperf_history), style = MaterialTheme.typography.labelLarge)
        Text(stringResource(R.string.netperf_history_note, NETPERF_HISTORY_LIMIT), style = MaterialTheme.typography.bodySmall, color = muted)
    }
    history.forEach { report ->
        Card(onClick = { onOpen(report) }, modifier = gutter.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    stringResource(R.string.netperf_summary, report.client, report.server, report.seconds),
                    style = MaterialTheme.typography.titleSmall,
                    fontFamily = FontFamily.Monospace,
                )
                Text(
                    listOfNotNull(testedAt(report), headline(report)).joinToString("  ·  "),
                    style = MaterialTheme.typography.bodySmall,
                    color = muted,
                )
            }
        }
    }
}

/** A saved test: when it ran and what it measured. Back returns to the setup. */
@Composable
internal fun NetPerfSaved(report: NetPerfReport, onBack: () -> Unit, onDelete: () -> Unit) {
    BackHandler(onBack = onBack)
    val gutter = Modifier.padding(horizontal = 16.dp)
    Column(gutter) {
        Text(
            stringResource(R.string.netperf_summary, report.client, report.server, report.seconds),
            style = MaterialTheme.typography.titleSmall,
            fontFamily = FontFamily.Monospace,
        )
        Text(testedAt(report), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    NetPerfResults(report.setup, report.results, running = false)
    Row(gutter.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedButton(onClick = onBack, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_back)) }
        OutlinedButton(onClick = onDelete, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_delete)) }
    }
}

private fun testedAt(report: NetPerfReport): String =
    DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(report.started))

/** Pod-to-pod throughput and p50 latency, the figures the list compares tests by. */
private fun headline(report: NetPerfReport): String? {
    val pod = report.results.filter { it.path == NETPERF_PATH_POD && it.error.isEmpty() }
    val throughput = pod.firstOrNull { it.test == NETPERF_THROUGHPUT }?.let { formatMbps(it.throughputMbps) }
    val latency = pod.firstOrNull { it.test == NETPERF_LATENCY }?.latency?.let { formatMicros(it.p50) }
    return listOfNotNull(throughput, latency).joinToString(" · ").ifEmpty { null }
}
