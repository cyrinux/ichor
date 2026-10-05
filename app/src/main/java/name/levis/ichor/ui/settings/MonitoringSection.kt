package name.levis.ichor.ui.settings

import name.levis.ichor.R
import androidx.compose.ui.res.stringResource
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
import name.levis.ichor.TalosApp
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.monitor.MonitorStore
import name.levis.ichor.monitor.canPostNotifications
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MonitoringSection(app: TalosApp) {
    val context = LocalContext.current
    val store = app.monitorStore
    val enabled by store.alertsEnabled.collectAsStateWithLifecycle()
    val dataWatched by store.dataServicesWatched.collectAsStateWithLifecycle()
    val gitopsWatched by store.gitopsWatched.collectAsStateWithLifecycle()
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
            error = context.getString(R.string.monitor_notifications_blocked)
        }
    }

    fun toggle(on: Boolean) {
        if (on && !canPostNotifications(context) && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            permission.launch(Manifest.permission.POST_NOTIFICATIONS)
        } else {
            apply(on)
        }
    }

    SectionTitle(stringResource(R.string.monitor_section))
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.monitor_background_alerts), style = MaterialTheme.typography.titleMedium)
                    MutedText(stringResource(R.string.monitor_background_alerts_desc, CERT_WARN_DAYS))
                }
                Switch(checked = enabled, onCheckedChange = ::toggle, modifier = Modifier.padding(start = 12.dp))
            }
            // Opt-in on top of the alerts: Kubernetes API calls and a Garage CLI run at every check.
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.monitor_data_services), style = MaterialTheme.typography.titleSmall)
                    MutedText(stringResource(R.string.monitor_data_services_desc))
                }
                Switch(
                    checked = dataWatched,
                    onCheckedChange = { store.setDataServicesWatched(it) },
                    enabled = enabled,
                    modifier = Modifier.padding(start = 12.dp),
                )
            }
            // Same for Argo CD and Flux apps: their custom resources are listed at every check.
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.monitor_gitops), style = MaterialTheme.typography.titleSmall)
                    MutedText(stringResource(R.string.monitor_gitops_desc))
                }
                Switch(
                    checked = gitopsWatched,
                    onCheckedChange = { store.setGitopsWatched(it) },
                    enabled = enabled,
                    modifier = Modifier.padding(start = 12.dp),
                )
            }
            Text(stringResource(R.string.monitor_check_every), style = MaterialTheme.typography.labelLarge)
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                MonitorStore.INTERVALS.forEachIndexed { index, minutes ->
                    SegmentedButton(
                        selected = minutes == interval,
                        onClick = {
                            store.setIntervalMinutes(minutes)
                            app.launchSync()
                        },
                        shape = SegmentedButtonDefaults.itemShape(index, MonitorStore.INTERVALS.size),
                    ) { Text(if (minutes < 60) stringResource(R.string.monitor_interval_minutes, minutes.toInt()) else stringResource(R.string.monitor_interval_hours, (minutes / 60).toInt())) }
                }
            }
            MutedText(stringResource(R.string.monitor_widget_hint))
            OutlinedButton(onClick = { app.launchSync(runNow = true) }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.common_check_now))
            }
            error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
        }
    }
}
