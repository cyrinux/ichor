package name.levis.ichor.ui.upgrade

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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.K8sUpgradeRunState
import name.levis.ichor.model.StepStatus
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The Kubernetes upgrade (or its dry run) as it goes: every step reported, the current one
 * spinning, Cancel; then how it ended. A dry run that passed offers the real run.
 */
@Composable
fun K8sUpgradeRunView(run: K8sUpgradeRunState, onCancel: () -> Unit, onUpgradeNow: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    var confirmingCancel by remember { mutableStateOf(false) }
    if (run.running) KeepScreenOn()

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            stringResource(if (run.dryRun) R.string.k8s_upgrade_dry_run_title else R.string.k8s_upgrade_notification_title, run.version),
            style = MaterialTheme.typography.titleMedium,
        )
        when {
            run.finished && run.error == null && run.dryRun -> Text(stringResource(R.string.k8s_upgrade_dry_run_done_detail), color = colors.ok)
            run.finished && run.error == null -> Text(stringResource(R.string.k8s_upgrade_done_all, run.version), color = colors.ok)
            run.finished -> Text(run.error.orEmpty().ifEmpty { stringResource(R.string.k8s_upgrade_no_answer) }, color = colors.bad)
            else -> MutedText(run.latest?.let { k8sStepText(context, it) } ?: stringResource(R.string.cluster_upgrade_starting))
        }

        if (run.events.isNotEmpty()) {
            SectionTitle(stringResource(R.string.k8s_upgrade_steps))
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    // One row per node and phase, as reported: the last one is the current step.
                    val rows = run.events.distinctBy { Triple(it.phase, it.node, it.component) }
                    rows.forEachIndexed { i, e ->
                        val last = i == rows.lastIndex
                        TimelineRow(
                            label = k8sStepText(context, e),
                            status = when {
                                !last -> StepStatus.DONE
                                run.running -> StepStatus.CURRENT
                                run.error != null -> StepStatus.FAILED
                                else -> StepStatus.DONE
                            },
                            at = e.at,
                            message = if (last) e.message else "",
                        )
                    }
                }
            }
        }

        when {
            run.running && run.cancelling -> MutedText(stringResource(R.string.k8s_upgrade_cancelling))
            run.running -> {
                OutlinedButton(onClick = { confirmingCancel = true }, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.common_cancel))
                }
                MutedText(stringResource(R.string.cluster_upgrade_leave_note))
            }
            run.dryRun && run.error == null -> {
                Button(onClick = onUpgradeNow, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.k8s_upgrade_now)) }
                OutlinedButton(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_close)) }
            }
            else -> Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_close)) }
        }
    }

    if (confirmingCancel) {
        ConfirmDialog(
            title = stringResource(R.string.k8s_upgrade_cancel_title),
            text = stringResource(R.string.k8s_upgrade_cancel_body),
            confirm = stringResource(R.string.k8s_upgrade_cancel_confirm),
            onConfirm = {
                confirmingCancel = false
                onCancel()
            },
            onDismiss = { confirmingCancel = false },
            destructive = true,
        )
    }
}
