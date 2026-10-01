package name.levis.talosmobile.ui.settings

import android.Manifest
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.monitor.CERT_WARN_DAYS
import name.levis.talosmobile.monitor.MonitorStore
import name.levis.talosmobile.monitor.canPostNotifications
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.theme.LocalStatusColors

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MonitoringSection(app: TalosApp) {
    val context = LocalContext.current
    val store = app.monitorStore
    val enabled by store.alertsEnabled.collectAsStateWithLifecycle()
    val interval by store.intervalMinutes.collectAsStateWithLifecycle()
    var error by remember { mutableStateOf<String?>(null) }

    fun apply(on: Boolean) {
        store.setAlertsEnabled(on)
        app.launchSync(runNow = on)
    }

    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) {
            error = null
            apply(true)
        } else {
            error = "Notifications are blocked for this app; allow them in Android settings."
        }
    }

    fun toggle(on: Boolean) {
        if (on && !canPostNotifications(context) && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            permission.launch(Manifest.permission.POST_NOTIFICATIONS)
        } else {
            apply(on)
        }
    }

    SectionTitle("Monitoring")
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Background alerts", style = MaterialTheme.typography.titleMedium)
                    Text(
                        "Notifies when a node goes down or recovers, on new etcd alarms, and daily when the " +
                            "client certificate expires within $CERT_WARN_DAYS days. Silent while the cluster " +
                            "is unreachable (e.g. off VPN).",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(checked = enabled, onCheckedChange = ::toggle, modifier = Modifier.padding(start = 12.dp))
            }
            Text("Check every", style = MaterialTheme.typography.labelLarge)
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                MonitorStore.INTERVALS.forEachIndexed { index, minutes ->
                    SegmentedButton(
                        selected = minutes == interval,
                        onClick = {
                            store.setIntervalMinutes(minutes)
                            app.launchSync()
                        },
                        shape = SegmentedButtonDefaults.itemShape(index, MonitorStore.INTERVALS.size),
                    ) { Text(if (minutes < 60) "$minutes min" else "${minutes / 60} h") }
                }
            }
            Text(
                "The home-screen widget uses the same checks; adding it keeps them running even with alerts off.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            OutlinedButton(onClick = { app.launchSync(runNow = true) }, modifier = Modifier.fillMaxWidth()) {
                Text("Check now")
            }
            error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
        }
    }
}
