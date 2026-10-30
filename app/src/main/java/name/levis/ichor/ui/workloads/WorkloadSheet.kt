package name.levis.ichor.ui.workloads

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.AssistChip
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
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.PinButton
import name.levis.ichor.ui.share.AddToHomeButton
import name.levis.ichor.ui.share.ShareLinkButton
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubeGreenNamespace
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeRevisionList
import name.levis.ichor.model.WorkloadState
import name.levis.ichor.model.canPauseRollout
import name.levis.ichor.ui.argocd.ArgoRevertDialog
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.WorkloadAutoscaler
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
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeActionAccess
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.util.timeAgo

/**
 * One workload's actions: restart, pause or resume a Deployment's rollout, a scale stepper
 * (Deployments and StatefulSets) and its revisions; a tap on one opens its diff and rollback
 * ([onRevision], with the history it belongs to).
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun WorkloadSheet(
    workload: KubeWorkload,
    actions: WorkloadActions,
    onRestart: () -> Unit,
    onRevision: (KubeRevision, KubeRevisionList) -> Unit,
    onDismiss: () -> Unit,
    /** Opens the list of its pods; null for a kind whose pods cannot be listed. */
    onPods: (() -> Unit)? = null,
    /** Opens the Autoscaling screen; null where the screen is not wired. */
    onAutoscaling: (() -> Unit)? = null,
) {
    val busy by actions.busy.collectAsStateWithLifecycle()
    // kube-green may have the namespace asleep: say it, and warn before a change it would undo.
    val kubeGreen = rememberKubeGreenNamespace(workload.namespace)
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(workload.name, style = MaterialTheme.typography.titleLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    Text("${workload.kind}  ·  ${workload.namespace}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    MutedText(stringResource(R.string.workloads_ready_count, workload.ready, workload.desired))
                }
                PinButton(ShareTarget.workload(workload.kind, workload.namespace, workload.name), workload.name)
                ShareLinkButton(ShareTarget.workload(workload.kind, workload.namespace, workload.name))
                AddToHomeButton(ShareTarget.workload(workload.kind, workload.namespace, workload.name), workload.name)
            }
            KubeGreenAsleepNote(kubeGreen, workload.kind)
            val access = rememberKubeActionAccess(workload.namespace)
            val restartDenial = access?.denial(KubeAction.restart(workload.kind))
            val pauseDenial = access?.denial(KubeAction.PAUSE_ROLLOUT)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = onRestart, enabled = workload.canRestart && restartDenial == null) {
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
                if (workload.canPauseRollout) {
                    PauseButton(workload, enabled = workload.key !in busy && pauseDenial == null) { actions.pauseRollout(workload, it) }
                }
            }
            KubeDenialNote(restartDenial)
            // A Deployment's pause needs the restart's patch: one note for both.
            if (workload.canPauseRollout && restartDenial == null) KubeDenialNote(pauseDenial)
            if (workload.canScale) {
                HorizontalDivider()
                ScaleSection(workload, actions, workload.key in busy, access?.denial(KubeAction.scale(workload.kind)), kubeGreen, onAutoscaling)
            }
            if (workload.hasHistory) {
                HorizontalDivider()
                HistorySection(workload, actions, workload.key in busy, access?.denial(KubeAction.rollback(workload.kind)), onRevision)
            }
            HorizontalDivider()
            Text(stringResource(R.string.kube_events_title), style = MaterialTheme.typography.titleSmall)
            KubeEventsList(workload.namespace, "", workload.name)
        }
    }
}

@Composable
private fun ScaleSection(workload: KubeWorkload, actions: WorkloadActions, busy: Boolean, denial: KubePermission?, kubeGreen: KubeGreenNamespace?, onAutoscaling: (() -> Unit)? = null) {
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
        if (freezeFirst && owner != null) actions.freezeThenScale(workload, replicas, owner, freezeReason)
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
    workload.autoscaler?.let { autoscaler ->
        AutoscalerChip(autoscaler, onAutoscaling)
        Text(stringResource(R.string.workloads_autoscaler_note), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    // kube-green restores the replicas it saved at the next wake-up: say so before the tap.
    val kubeGreenWarning = kubeGreenChangeWarning(kubeGreen)
    if (kubeGreenWarning != null && target != workload.desired) {
        Text(kubeGreenWarning, color = colors.warn, style = MaterialTheme.typography.bodySmall)
    }
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
                enabled = target != workload.desired && denial == null,
            ) { Text(stringResource(R.string.workloads_scale_apply)) }
        }
    }
    KubeDenialNote(denial)
    outcome?.takeIf { it.workloadKey == workload.key }?.let { o ->
        o.error?.let { InlineError(it.asString()) }
        if (o.warning.isNotEmpty()) Text(o.warning, color = colors.warn, style = MaterialTheme.typography.bodySmall)
    }

    if (askArgo && argoOwner != null) {
        ArgoRevertDialog(
            argoOwner,
            title = stringResource(R.string.argo_revert_title),
            text = "",
            confirm = stringResource(R.string.argo_revert_anyway),
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

@Composable
private fun HistorySection(workload: KubeWorkload, actions: WorkloadActions, busy: Boolean, denial: KubePermission?, onRevision: (KubeRevision, KubeRevisionList) -> Unit) {
    // Read again when the workload rolls out a new template.
    val revisions by produceState<UiState<KubeRevisionList>>(UiState.Loading, workload.key, workload.updated, workload.desired, workload.state) {
        value = uiStateOf { actions.revisions(workload) }
    }
    SectionTitle(stringResource(R.string.workloads_history))
    KubeDenialNote(denial)
    when (val s = revisions) {
        UiState.Loading -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
        is UiState.Failed -> InlineError(s.message.asString())
        is UiState.Loaded -> {
            val list = s.data
            if (list.revisions.isEmpty()) MutedText(stringResource(R.string.workloads_history_empty))
            if (list.manual && list.revisions.size > 1) MutedText(stringResource(R.string.revision_diff_manual))
            list.revisions.forEach { RevisionRow(it, enabled = !busy, onOpen = { onRevision(it, list) }) }
        }
    }
}

@Composable
private fun RevisionRow(revision: KubeRevision, enabled: Boolean, onOpen: () -> Unit) {
    val colors = LocalStatusColors.current
    // An older revision opens its diff and rollback; the current one has nothing to undo.
    val open = if (revision.current) Modifier else Modifier.clickable(enabled = enabled, role = Role.Button, onClick = onOpen)
    Row(Modifier.fillMaxWidth().then(open), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.workloads_revision, revision.revision), style = MaterialTheme.typography.bodyMedium)
                if (revision.created > 0) {
                    MutedText(timeAgo(revision.created))
                }
                if (revision.current) StatusPill(stringResource(R.string.workloads_revision_current), colors.ok)
            }
            revision.images.forEach {
                Text(it, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (revision.changeCause.isNotEmpty()) MutedText(revision.changeCause)
        }
        if (!revision.current) {
            TextButton(onClick = onOpen, enabled = enabled) { Text(stringResource(R.string.workloads_rollback)) }
        }
    }
}

/** Pause a Deployment's rollout, or resume a paused one ([onPause] with the new state). */
@Composable
private fun PauseButton(workload: KubeWorkload, enabled: Boolean, onPause: (Boolean) -> Unit) {
    val paused = workload.workloadState == WorkloadState.PAUSED
    OutlinedButton(onClick = { onPause(!paused) }, enabled = enabled) {
        Icon(if (paused) Icons.Outlined.PlayArrow else Icons.Outlined.Pause, contentDescription = null, modifier = Modifier.size(ButtonDefaults.IconSize))
        Spacer(Modifier.size(ButtonDefaults.IconSpacing))
        Text(stringResource(if (paused) R.string.rollout_resume else R.string.rollout_pause))
    }
}

/** "Autoscaled by HPA web-hpa · 3 of 1–5", in the state's colour; a tap opens the Autoscaling screen. */
@Composable
private fun AutoscalerChip(autoscaler: WorkloadAutoscaler, onAutoscaling: (() -> Unit)?) {
    val colors = LocalStatusColors.current
    val label = stringResource(R.string.workloads_autoscaled_by, autoscaler.label())
    AssistChip(
        onClick = { onAutoscaling?.invoke() },
        label = { Text(label, style = MaterialTheme.typography.labelMedium) },
        leadingIcon = {
            Icon(
                Icons.Outlined.Speed,
                contentDescription = null,
                modifier = Modifier.size(18.dp),
                tint = if (autoscaler.stateOk) MaterialTheme.colorScheme.onSurfaceVariant else colors.warn
            )
        },
        modifier = Modifier.semantics { contentDescription = label },
    )
}
