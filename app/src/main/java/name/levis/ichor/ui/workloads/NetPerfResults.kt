package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NETPERF_LATENCY
import name.levis.ichor.model.NETPERF_PATH_HOST
import name.levis.ichor.model.NETPERF_PATH_POD
import name.levis.ichor.model.NETPERF_THROUGHPUT
import name.levis.ichor.model.NetPerfResult
import name.levis.ichor.model.NetPerfSetup
import name.levis.ichor.model.formatMbps
import name.levis.ichor.model.formatMicros
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.components.InlineError
import java.text.NumberFormat

/** One card per network path, with its throughput and latency once measured. */
@Composable
fun NetPerfResults(setup: NetPerfSetup, results: List<NetPerfResult>, running: Boolean) {
    val paths = if (setup.hostNetwork) listOf(NETPERF_PATH_POD, NETPERF_PATH_HOST) else listOf(NETPERF_PATH_POD)
    paths.forEach { path ->
        val measured = results.filter { it.path == path }
        if (measured.isEmpty() && !running) return@forEach
        Card(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(pathLabel(path), style = MaterialTheme.typography.titleSmall)
                    InfoHint(stringResource(R.string.netperf_hint_paths_title), stringResource(R.string.netperf_hint_paths))
                }
                listOf(NETPERF_THROUGHPUT, NETPERF_LATENCY).forEach { test ->
                    val result = measured.firstOrNull { it.test == test }
                    if (result != null || running) ResultRow(test, result)
                }
            }
        }
    }
}

@Composable
private fun ResultRow(test: String, result: NetPerfResult?) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(testLabel(test), style = MaterialTheme.typography.bodyMedium, color = muted)
            InfoHint(testLabel(test), stringResource(if (test == NETPERF_LATENCY) R.string.netperf_hint_latency else R.string.netperf_hint_throughput))
            Spacer(Modifier.weight(1f))
            Text(
                when {
                    result == null -> "…"
                    result.error.isNotEmpty() -> "—"
                    test == NETPERF_THROUGHPUT -> formatMbps(result.throughputMbps)
                    else -> result.latency?.let { formatMicros(it.p50) } ?: "—"
                },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
        }
        when {
            result == null -> Unit
            result.error.isNotEmpty() -> InlineError(result.error)
            test == NETPERF_LATENCY -> result.latency?.let { l ->
                Text(
                    stringResource(R.string.netperf_latency_values, formatMicros(l.p50), formatMicros(l.p90), formatMicros(l.p99)) + "  ·  " +
                        stringResource(R.string.netperf_round_trips, NumberFormat.getIntegerInstance().format(result.transactionRate)),
                    style = MaterialTheme.typography.labelSmall,
                    color = muted,
                )
                NetPerfLatencyRange(l)
            }
        }
    }
}

@Composable
internal fun pathLabel(path: String): String =
    stringResource(if (path == NETPERF_PATH_HOST) R.string.netperf_path_host else R.string.netperf_path_pod)

@Composable
internal fun testLabel(test: String): String =
    stringResource(if (test == NETPERF_LATENCY) R.string.netperf_test_latency else R.string.netperf_test_throughput)
