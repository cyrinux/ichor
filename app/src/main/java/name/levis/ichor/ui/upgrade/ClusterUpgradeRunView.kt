package name.levis.ichor.ui.upgrade

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.ClusterUpgradeRunState
import name.levis.ichor.model.ClusterUpgradeNode
import name.levis.ichor.model.ClusterUpgradeProgress
import name.levis.ichor.model.StepStatus
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The roll as it goes: where it stands, every node in order (the one being upgraded with its
 * phase), Pause / Resume / Abort, then how it ended. Leaving the screen keeps it going.
 */
@Composable
fun ClusterUpgradeRunView(run: ClusterUpgradeRunState, onPause: () -> Unit, onResume: () -> Unit, onAbort: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    var confirmingAbort by remember { mutableStateOf(false) }
    val progress = run.progress
    if (run.running) KeepScreenOn()

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.cluster_upgrade_notification_title, run.version), style = MaterialTheme.typography.titleMedium)
        when {
            run.finished && run.error == null -> Text(stringResource(R.string.cluster_upgrade_done_all, run.version), color = colors.ok)
            run.finished -> Text(run.error.orEmpty().ifEmpty { stringResource(R.string.cluster_upgrade_no_answer) }, color = colors.bad)
            progress == null || progress.total == 0 -> MutedText(stringResource(R.string.cluster_upgrade_starting))
            else -> {
                Text(
                    stringResource(R.string.cluster_upgrade_step, (progress.index + 1).coerceAtMost(progress.total), progress.total, progress.name, phaseText(context, progress)),
                    style = MaterialTheme.typography.bodyLarge,
                )
                // Why it paused (a failed gate), or the gate's own progress.
                if (progress.message.isNotBlank()) {
                    Text(progress.message, color = if (run.paused) colors.warn else MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }

        SectionTitle(stringResource(R.string.cluster_upgrade_nodes))
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                run.nodes.forEach { node ->
                    val current = progress?.node == node.node && run.running
                    TimelineRow(
                        label = node.name,
                        status = when (node.state) {
                            ClusterUpgradeNode.DONE -> StepStatus.DONE
                            ClusterUpgradeNode.FAILED -> StepStatus.FAILED
                            ClusterUpgradeNode.RUNNING -> StepStatus.CURRENT
                            else -> StepStatus.PENDING
                        },
                        at = 0,
                        message = if (current && progress != null && progress.phase == ClusterUpgradeProgress.NODE) phaseText(context, progress) else "",
                    )
                }
            }
        }

        if (run.running) {
            if (run.aborting) {
                MutedText(stringResource(R.string.cluster_upgrade_aborting))
            } else {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth()) {
                    if (run.paused) {
                        Button(onClick = onResume, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.cluster_upgrade_resume)) }
                    } else {
                        OutlinedButton(onClick = onPause, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.cluster_upgrade_pause)) }
                    }
                    OutlinedButton(onClick = { confirmingAbort = true }, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.cluster_upgrade_abort)) }
                }
            }
            MutedText(stringResource(R.string.cluster_upgrade_leave_note))
        } else {
            Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_close)) }
        }
    }

    if (confirmingAbort) {
        ConfirmDialog(
            title = stringResource(R.string.cluster_upgrade_abort_title),
            text = stringResource(R.string.cluster_upgrade_abort_body),
            confirm = stringResource(R.string.cluster_upgrade_abort),
            onConfirm = {
                confirmingAbort = false
                onAbort()
            },
            onDismiss = { confirmingAbort = false },
            destructive = true,
        )
    }
}
