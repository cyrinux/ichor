package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.model.HetznerOptions
import name.levis.ichor.model.HetznerSnapshotCost
import name.levis.ichor.model.HetznerSnapshotSpec
import name.levis.ichor.model.JoinPhase
import name.levis.ichor.model.joinTimeline
import name.levis.ichor.model.locationsFor
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.upgrade.TimelineRow

private val snapshotArchitectures = listOf("amd64", "arm64")

/**
 * "Make a Talos snapshot": the architecture (that of the server type asked for, editable), the
 * location and the cost line, then a plain confirmation: nothing of the cluster is touched and the
 * server is deleted afterwards.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun HetznerSnapshotSheet(
    options: HetznerOptions,
    initialArch: String,
    initialLocation: String,
    template: String,
    estimate: suspend (arch: String, location: String) -> HetznerSnapshotCost,
    onStart: (HetznerSnapshotSpec) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    var arch by remember { mutableStateOf(initialArch) }
    var chosenLocation by remember { mutableStateOf(initialLocation) }
    var cost by remember { mutableStateOf<UiState<HetznerSnapshotCost>?>(null) }
    var retry by remember { mutableStateOf(0) }
    val locations = options.locationsFor(arch)
    val location = chosenLocation.takeIf { name -> locations.any { it.name == name } }.orEmpty()

    LaunchedEffect(arch, location, retry) {
        if (location.isBlank()) {
            cost = null
        } else {
            cost = UiState.Loading
            cost = uiStateOf { estimate(arch, location) }
        }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.hetzner_snapshot_make)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MutedText(stringResource(R.string.hetzner_snapshot_intro))
                Text(stringResource(R.string.hetzner_snapshot_arch), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    snapshotArchitectures.forEach { a ->
                        IchorFilterChip(selected = a == arch, onClick = { arch = a }, label = { Text(a) })
                    }
                }
                Text(stringResource(R.string.hetzner_location), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    locations.forEach { l ->
                        IchorFilterChip(selected = l.name == location, onClick = { chosenLocation = l.name }, label = { Text("${l.name} · ${l.city}") })
                    }
                }
                when (val state = cost) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, { retry++ })
                    is UiState.Loaded -> Text(
                        stringResource(
                            R.string.hetzner_snapshot_cost,
                            state.data.serverType,
                            "${state.data.hourly} ${state.data.currency}",
                            "${state.data.monthlyPerGB} ${state.data.currency}",
                        ),
                        color = colors.warn,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
        },
        confirmButton = {
            IchorButton(
                onClick = { onStart(HetznerSnapshotSpec(templateNode = template, arch = arch, location = location)) },
                enabled = cost is UiState.Loaded,
            ) { Text(stringResource(R.string.hetzner_snapshot_confirm)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The followed snapshot run: its steps with the command's last line, Stop, then the result. */
@Composable
internal fun SnapshotRunView(run: JoinRunState, onStop: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    var confirmStop by remember { mutableStateOf(false) }
    // A failure stays on the step that failed, not on the cleanup that followed: the message says what was deleted.
    val steps = remember(run.events, run.finished, run.error) {
        val events = if (run.error != null) run.events.filterNot { it.phase == JoinPhase.CLEANUP.wire } else run.events
        joinTimeline(events, run.finished, run.error != null, run.request.phases)
    }
    if (run.running) KeepScreenOn()

    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            steps.forEach { TimelineRow(stringResource(it.phase.label), it.status, it.at, it.message) }
        }
    }
    when {
        run.running -> {
            MutedText(stringResource(R.string.hetzner_snapshot_stop_hint))
            IchorOutlinedButton(onClick = { confirmStop = true }, enabled = !run.stopping, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.hetzner_snapshot_stop))
            }
        }
        else -> {
            if (run.error != null) {
                Text(run.error, color = colors.bad)
            } else {
                Text(
                    stringResource(R.string.hetzner_snapshot_done, run.snapshotResult?.description.orEmpty()),
                    color = colors.ok,
                    style = MaterialTheme.typography.titleMedium,
                )
            }
            IchorButton(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
        }
    }

    if (confirmStop) {
        ConfirmDialog(
            title = stringResource(R.string.hetzner_snapshot_stop),
            text = stringResource(R.string.hetzner_snapshot_stop_body),
            confirm = stringResource(R.string.hetzner_snapshot_stop),
            onConfirm = {
                confirmStop = false
                onStop()
            },
            onDismiss = { confirmStop = false },
            destructive = true,
        )
    }
}
