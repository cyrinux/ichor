package name.levis.ichor.ui.live

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.Bottlenecks
import name.levis.ichor.util.formatBytes
import java.util.Locale

@Composable
fun BottleneckDetails(detail: Bottlenecks) {
    fun number(n: Double) = String.format(Locale.ROOT, "%.1f", n)
    fun rate(n: Double) = formatBytes(n.toLong()) + "/s"
    Card {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.insights_bottlenecks), style = MaterialTheme.typography.titleMedium)
            Text(stringResource(R.string.insights_cpu_wait, number(detail.wait), number(detail.steal)))
            detail.disks.forEach { disk ->
                Text(disk.name, style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 12.dp))
                Text(stringResource(R.string.insights_disk_rate, rate(disk.read), rate(disk.write), number(disk.busy), number(disk.latency)))
            }
            detail.network.forEach { net ->
                Text(net.name, style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 12.dp))
                Text(stringResource(R.string.insights_net_rate, rate(net.read), rate(net.write), number(net.errors), number(net.drops)))
            }
            Text(stringResource(R.string.insights_metrics_note), style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 12.dp))
            detail.errors.forEach { (section, error) -> Text("$section: $error", color = MaterialTheme.colorScheme.error) }
        }
    }
}
