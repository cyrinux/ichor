package name.levis.ichor.ui.maintenance

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.MaintenanceRunState
import name.levis.ichor.model.DrainPod
import name.levis.ichor.model.maintenanceTimeline
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.upgrade.TimelineRow

/** The followed maintenance: phase timeline, the pods being evicted, then the result. */
@Composable
fun MaintenanceRunView(run: MaintenanceRunState, onStop: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    var confirmStop by remember { mutableStateOf(false) }
    val steps = remember(run.action, run.events, run.finished, run.error) {
        maintenanceTimeline(run.action, run.events, run.finished, run.error != null)
    }
    if (run.running) KeepScreenOn()

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                steps.forEach { TimelineRow(stringResource(it.phase.label), it.status, it.at, it.message) }
            }
        }
        if (run.pods.isNotEmpty()) {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    val gone = run.pods.count { it.state == DrainPod.STATE_GONE }
                    SectionTitle(stringResource(R.string.maintenance_pods_progress, gone, run.pods.size))
                    run.pods.forEach { DrainPodRow(it) }
                }
            }
        }
        when {
            run.running && run.stopping -> MutedText(stringResource(R.string.maintenance_stopping, run.hostname))
            run.running -> {
                MutedText(stringResource(R.string.maintenance_stop_hint))
                OutlinedButton(onClick = { confirmStop = true }, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.maintenance_stop))
                }
            }
            run.error != null -> {
                Text(stringResource(R.string.maintenance_failed, run.error), color = colors.bad)
                Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
            }
            else -> {
                Text(stringResource(run.doneText, run.hostname), color = colors.ok, style = MaterialTheme.typography.titleMedium)
                Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
            }
        }
    }

    if (confirmStop) {
        ConfirmDialog(
            title = stringResource(R.string.maintenance_stop_title),
            text = stringResource(R.string.maintenance_stop_body, run.hostname),
            confirm = stringResource(R.string.maintenance_stop),
            onConfirm = {
                confirmStop = false
                onStop()
            },
            onDismiss = { confirmStop = false },
            destructive = true,
        )
    }
}
