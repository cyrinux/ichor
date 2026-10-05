package name.levis.ichor.ui.workloads

import android.text.format.DateUtils
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.MAX_SCALE_REPLICAS
import name.levis.ichor.model.canScale
import name.levis.ichor.model.clampReplicas
import name.levis.ichor.model.hasHistory
import name.levis.ichor.model.scaleNeedsConfirm
import name.levis.ichor.model.scaleNeedsTypedName
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/**
 * One workload's actions: restart, a scale stepper (Deployments and StatefulSets) and, for a
 * Deployment, its revisions with a rollback ([onRollback] confirms first).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WorkloadSheet(
    workload: KubeWorkload,
    actions: WorkloadActions,
    onRestart: () -> Unit,
    onRollback: (KubeRevision) -> Unit,
    onDismiss: () -> Unit,
    /** Opens the list of its pods; null for a kind whose pods cannot be listed. */
    onPods: (() -> Unit)? = null,
) {
    val busy by actions.busy.collectAsStateWithLifecycle()
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(workload.name, style = MaterialTheme.typography.titleLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Text("${workload.kind}  ·  ${workload.namespace}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                MutedText(stringResource(R.string.workloads_ready_count, workload.ready, workload.desired))
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = onRestart, enabled = workload.canRestart) {
                    Icon(Icons.Outlined.RestartAlt, contentDescription = null, modifier = Modifier.size(ButtonDefaults.IconSize))
                    Spacer(Modifier.size(ButtonDefaults.IconSpacing))
                    Text(stringResource(R.string.workloads_restart_confirm))
                }
                if (onPods != null) {
                    OutlinedButton(onClick = onPods) {
                        Icon(Icons.Outlined.ViewInAr, contentDescription = null, modifier = Modifier.size(ButtonDefaults.IconSize))
                        Spacer(Modifier.size(ButtonDefaults.IconSpacing))
                        Text(stringResource(R.string.pods_title))
                    }
                }
            }
            if (workload.canScale) {
                HorizontalDivider()
                ScaleSection(workload, actions, workload.key in busy)
            }
            if (workload.hasHistory) {
                HorizontalDivider()
                HistorySection(workload, actions, workload.key in busy, onRollback)
            }
        }
    }
}

@Composable
private fun ScaleSection(workload: KubeWorkload, actions: WorkloadActions, busy: Boolean) {
    val colors = LocalStatusColors.current
    val outcome by actions.lastScale.collectAsStateWithLifecycle()
    // Starts again from the live count once a refresh brings it.
    var target by rememberSaveable(workload.key, workload.desired) { mutableIntStateOf(workload.desired) }
    var confirmZero by remember { mutableStateOf(false) }
    var confirmDown by remember { mutableStateOf(false) }
    // Argo CD self-heal would put the count back: ask first, offering to freeze the app.
    val argoOwner = remember(workload.key, busy) { actions.argoOwner(workload) }
    var askArgo by remember { mutableStateOf(false) }
    var freezeFirst by remember { mutableStateOf(false) }
    // Stored on the cluster: no name, which may be masked on screen.
    val freezeReason = stringResource(R.string.argo_freeze_reason_scale, target)
    val apply = { replicas: Int ->
        val owner = argoOwner
        if (freezeFirst && owner != null) actions.freezeThenScale(workload, replicas, owner.first, owner.second, freezeReason)
        else actions.scale(workload, replicas)
    }
    val proceed = {
        when {
            scaleNeedsTypedName(target) -> confirmZero = true
            scaleNeedsConfirm(workload.desired, target) -> confirmDown = true
            else -> apply(target)
        }
    }

    SectionTitle(stringResource(R.string.workloads_scale))
    Row(verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { target = clampReplicas(target - 1) }, enabled = target > 0 && !busy) {
            Icon(Icons.Outlined.Remove, stringResource(R.string.workloads_scale_less))
        }
        Text(
            stringResource(R.string.workloads_scale_change, workload.desired, target),
            style = MaterialTheme.typography.titleMedium,
            fontFamily = FontFamily.Monospace,
        )
        IconButton(onClick = { target = clampReplicas(target + 1) }, enabled = target < MAX_SCALE_REPLICAS && !busy) {
            Icon(Icons.Outlined.Add, stringResource(R.string.workloads_scale_more))
        }
        Spacer(Modifier.weight(1f))
        if (busy) {
            CircularProgressIndicator(Modifier.padding(12.dp).size(24.dp), strokeWidth = 2.dp)
        } else {
            Button(
                onClick = { if (argoOwner != null) askArgo = true else proceed() },
                enabled = target != workload.desired,
            ) { Text(stringResource(R.string.workloads_scale_apply)) }
        }
    }
    outcome?.takeIf { it.workloadKey == workload.key }?.let { o ->
        o.error?.let { InlineError(it.asString()) }
        if (o.warning.isNotEmpty()) Text(o.warning, color = colors.warn, style = MaterialTheme.typography.bodySmall)
    }

    if (askArgo && argoOwner != null) {
        ArgoRevertDialog(
            argoOwner.first,
            onFreezeFirst = { askArgo = false; freezeFirst = true; proceed() },
            onAnyway = { askArgo = false; freezeFirst = false; proceed() },
            onDismiss = { askArgo = false },
        )
    }
    if (confirmDown) {
        ConfirmDialog(
            title = stringResource(R.string.workloads_scale_down_title, workload.name, target),
            text = stringResource(R.string.workloads_scale_down_text, workload.desired - target, workload.namespace),
            confirm = stringResource(R.string.workloads_scale_apply),
            onConfirm = {
                confirmDown = false
                apply(target)
            },
            onDismiss = { confirmDown = false },
            destructive = true,
        )
    }
    if (confirmZero) {
        HostnameConfirmDialog(
            title = stringResource(R.string.workloads_scale_zero_title, workload.name),
            hostname = workload.name,
            confirmLabel = stringResource(R.string.workloads_scale_zero_confirm),
            onConfirm = {
                confirmZero = false
                apply(0)
            },
            onDismiss = { confirmZero = false },
        ) {
            Text(stringResource(R.string.workloads_scale_zero_text, workload.namespace), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** Argo CD [app] self-heals the workload: scaling by hand is undone within minutes unless it is frozen. */
@Composable
private fun ArgoRevertDialog(app: ArgoApp, onFreezeFirst: () -> Unit, onAnyway: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Outlined.AcUnit, contentDescription = null) },
        title = { Text(stringResource(R.string.argo_revert_title)) },
        text = { Text(stringResource(R.string.argo_revert_text, app.name)) },
        confirmButton = {
            Column(horizontalAlignment = Alignment.End) {
                TextButton(onClick = onFreezeFirst) { Text(stringResource(R.string.argo_revert_freeze, app.name)) }
                TextButton(onClick = onAnyway) { Text(stringResource(R.string.argo_revert_anyway)) }
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
            }
        },
    )
}

@Composable
private fun HistorySection(workload: KubeWorkload, actions: WorkloadActions, busy: Boolean, onRollback: (KubeRevision) -> Unit) {
    // Read again when the Deployment rolls out a new template.
    val revisions by produceState<UiState<List<KubeRevision>>>(UiState.Loading, workload.key, workload.updated, workload.desired) {
        value = uiStateOf { actions.revisions(workload) }
    }
    SectionTitle(stringResource(R.string.workloads_history))
    when (val s = revisions) {
        UiState.Loading -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
        is UiState.Failed -> InlineError(s.message.asString())
        is UiState.Loaded -> {
            if (s.data.isEmpty()) MutedText(stringResource(R.string.workloads_history_empty))
            s.data.forEach { RevisionRow(it, enabled = !busy, onRollback = { onRollback(it) }) }
        }
    }
}

@Composable
private fun RevisionRow(revision: KubeRevision, enabled: Boolean, onRollback: () -> Unit) {
    val colors = LocalStatusColors.current
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.workloads_revision, revision.revision), style = MaterialTheme.typography.bodyMedium)
                if (revision.created > 0) {
                    MutedText(DateUtils.getRelativeTimeSpanString(revision.created, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS).toString())
                }
                if (revision.current) StatusPill(stringResource(R.string.workloads_revision_current), colors.ok)
            }
            revision.images.forEach {
                Text(it, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (revision.changeCause.isNotEmpty()) MutedText(revision.changeCause)
        }
        if (!revision.current) {
            TextButton(onClick = onRollback, enabled = enabled) { Text(stringResource(R.string.workloads_rollback)) }
        }
    }
}
