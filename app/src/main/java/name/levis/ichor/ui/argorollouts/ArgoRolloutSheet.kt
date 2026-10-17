package name.levis.ichor.ui.argorollouts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoRollout
import name.levis.ichor.model.ArgoRolloutAction
import name.levis.ichor.model.KubeAction
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * One rollout: its phase and steps, why it is paused, the stable and new versions, the replicas,
 * the analysis and the images, then the actions its state allows, each also gated by what the
 * credentials may do, each confirmed (Abort in red).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArgoRolloutSheet(rollout: ArgoRollout, busy: Boolean, actions: ArgoRolloutActions, onDismiss: () -> Unit) {
    var confirming by remember(rollout.key) { mutableStateOf<ArgoRolloutAction?>(null) }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                HealthDot(rollout.serviceHealth)
                Text(rollout.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
                StrategyTag(rollout)
            }
            MutedText(rollout.namespace)
            Text(listOfNotNull(rollout.phase.ifEmpty { null }, argoRolloutStepText(rollout)).joinToString("  ·  "), style = MaterialTheme.typography.bodyMedium)
            if (rollout.message.isNotEmpty()) MutedText(rollout.message)
            if (rollout.steps.isNotEmpty()) ArgoRolloutStepBar(rollout, Modifier.padding(vertical = 4.dp))
            ArgoRolloutPauseLine(rollout)
            if (rollout.awaitsPromotion) {
                Text(stringResource(R.string.argo_rollout_waits_promotion), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn)
            }
            Details(rollout)
            ActionRow(rollout, busy, onAction = { confirming = it })
        }
    }
    confirming?.let { action ->
        val (title, text) = action.confirmation
        ConfirmDialog(
            title = stringResource(title, rollout.name),
            text = stringResource(text),
            confirm = stringResource(action.label),
            destructive = action == ArgoRolloutAction.ABORT,
            onConfirm = {
                confirming = null
                actions.run(rollout, action)
            },
            onDismiss = { confirming = null },
        )
    }
}

@Composable
private fun Details(rollout: ArgoRollout) {
    if (rollout.stableHash.isNotEmpty() || rollout.currentHash.isNotEmpty()) {
        MutedText(stringResource(R.string.argo_rollout_hashes, rollout.stableHash.ifEmpty { "-" }, rollout.currentHash.ifEmpty { "-" }))
    }
    MutedText(stringResource(R.string.argo_rollout_replicas, rollout.ready.toString(), rollout.replicas.toString(), rollout.updated.toString()))
    if (rollout.workloadRef.isNotEmpty()) MutedText(stringResource(R.string.argo_rollout_workload, rollout.workloadRef))
    rollout.analysis?.let { a ->
        SectionTitle(stringResource(R.string.argo_rollout_analysis), Modifier.padding(top = 8.dp))
        val color = when {
            a.failed -> LocalStatusColors.current.bad
            a.status == "Inconclusive" -> LocalStatusColors.current.warn
            else -> Color.Unspecified
        }
        InfoRow(a.name.ifEmpty { "-" }, a.status.ifEmpty { "-" }, mono = true, valueColor = color)
        if (a.message.isNotEmpty()) MutedText(a.message)
    }
    if (rollout.images.isNotEmpty()) {
        SectionTitle(stringResource(R.string.apps_detail_images), Modifier.padding(top = 8.dp))
        rollout.images.forEach { Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
    }
}

/** The actions the state allows, in a wrapping row; off while one runs or when the credentials may not. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ActionRow(rollout: ArgoRollout, busy: Boolean, onAction: (ArgoRolloutAction) -> Unit) {
    val shown = ArgoRolloutAction.entries.filter(rollout::allows)
    if (shown.isEmpty()) return
    val denial = rememberKubeDenial(KubeAction.ARGO_ROLLOUT, rollout.namespace)
    SectionTitle(stringResource(R.string.vm_actions), Modifier.padding(top = 8.dp))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        shown.forEach { action ->
            val colors = if (action == ArgoRolloutAction.ABORT) {
                ButtonDefaults.outlinedButtonColors(contentColor = LocalStatusColors.current.bad)
            } else {
                ButtonDefaults.outlinedButtonColors()
            }
            OutlinedButton(onClick = { onAction(action) }, enabled = !busy && denial == null, colors = colors) {
                Text(stringResource(action.label))
            }
        }
    }
    KubeDenialNote(denial)
    if (busy) MutedText(stringResource(R.string.vm_action_running))
}
