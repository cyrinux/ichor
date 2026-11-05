package name.levis.ichor.ui.join

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
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
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.model.ImageRunKind
import name.levis.ichor.model.JoinPhase
import name.levis.ichor.model.joinTimeline
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.upgrade.TimelineRow

/**
 * The followed image run (a Hetzner snapshot, an AWS AMI or an Azure image): its steps with the
 * command's last line, Stop, then the result.
 */
@Composable
internal fun ImageRunView(run: JoinRunState, onStop: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    val kind = run.request.imageKind ?: ImageRunKind.HETZNER_SNAPSHOT
    var confirmStop by remember { mutableStateOf(false) }
    // A failure stays on the step that failed, not on the cleanup that followed: the message says what was deleted.
    val steps = remember(run.events, run.finished, run.error) {
        val events = if (run.error != null) run.events.filterNot { it.phase == JoinPhase.CLEANUP.wire } else run.events
        joinTimeline(events, run.finished, run.error != null, run.request.phases)
    }
    if (run.running) KeepScreenOn()

    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            steps.forEach { TimelineRow(stringResource(kind.label(it.phase)), it.status, it.at, it.message) }
        }
    }
    when {
        run.running -> {
            MutedText(stringResource(kind.stopHint))
            IchorOutlinedButton(onClick = { confirmStop = true }, enabled = !run.stopping, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.hetzner_snapshot_stop))
            }
        }
        else -> {
            if (run.error != null) {
                Text(run.error, color = colors.bad)
            } else {
                Text(stringResource(kind.done, kind.madeName(run)), color = colors.ok, style = MaterialTheme.typography.titleMedium)
            }
            IchorButton(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
        }
    }

    if (confirmStop) {
        ConfirmDialog(
            title = stringResource(R.string.hetzner_snapshot_stop),
            text = stringResource(kind.stopBody),
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

/** A step's label: a helper instance or VM does the work of AWS's and Azure's runs, not a server with a key. */
@StringRes
internal fun ImageRunKind.label(phase: JoinPhase): Int = when (this) {
    ImageRunKind.HETZNER_SNAPSHOT -> phase.label
    ImageRunKind.AWS_AMI -> when (phase) {
        JoinPhase.CREATING -> R.string.aws_phase_helper
        JoinPhase.REGISTER -> R.string.aws_phase_register
        JoinPhase.CLEANUP -> R.string.aws_phase_cleanup
        else -> phase.label
    }
    ImageRunKind.AZURE_IMAGE -> when (phase) {
        JoinPhase.CREATING -> R.string.azure_phase_helper
        JoinPhase.SNAPSHOT -> R.string.azure_phase_image
        JoinPhase.CLEANUP -> R.string.azure_phase_cleanup
        else -> phase.label
    }
}

@get:StringRes
private val ImageRunKind.stopHint: Int
    get() = when (this) {
        ImageRunKind.HETZNER_SNAPSHOT -> R.string.hetzner_snapshot_stop_hint
        ImageRunKind.AWS_AMI -> R.string.aws_image_stop_hint
        ImageRunKind.AZURE_IMAGE -> R.string.azure_image_stop_hint
    }

@get:StringRes
private val ImageRunKind.stopBody: Int
    get() = when (this) {
        ImageRunKind.HETZNER_SNAPSHOT -> R.string.hetzner_snapshot_stop_body
        ImageRunKind.AWS_AMI -> R.string.aws_image_stop_body
        ImageRunKind.AZURE_IMAGE -> R.string.azure_image_stop_body
    }

/** "Snapshot %1$s is ready…", with [madeName] as its argument. */
@get:StringRes
internal val ImageRunKind.done: Int
    get() = when (this) {
        ImageRunKind.HETZNER_SNAPSHOT -> R.string.hetzner_snapshot_done
        ImageRunKind.AWS_AMI -> R.string.aws_image_done
        ImageRunKind.AZURE_IMAGE -> R.string.azure_image_done
    }

/** What the finished run made, as [done] names it. */
internal fun ImageRunKind.madeName(run: JoinRunState): String = when (this) {
    ImageRunKind.HETZNER_SNAPSHOT -> run.snapshotResult?.description.orEmpty()
    ImageRunKind.AWS_AMI, ImageRunKind.AZURE_IMAGE -> run.madeImage?.name.orEmpty()
}
