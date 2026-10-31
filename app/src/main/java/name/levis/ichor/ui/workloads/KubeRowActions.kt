package name.levis.ichor.ui.workloads

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Article
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.OpenInFull
import androidx.compose.material.icons.outlined.PauseCircle
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.PlayCircle
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.Stream
import androidx.compose.material3.CardDefaults
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubeCronJob
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.canScale
import name.levis.ichor.ui.components.QuickActionRow
import name.levis.ichor.ui.components.RowAction
import name.levis.ichor.ui.components.denialReason
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.share.rememberPinAction
import name.levis.ichor.ui.share.rememberShareAction

/**
 * A pod row's quick actions: swipe right for its logs, left to delete it (the confirmation
 * first); a long press lists Logs, Live flows (with Hubble), Pin, Share and Delete. [background]
 * is the colour the row is drawn on, so the swipe's colour shows only beside it.
 */
@Composable
internal fun PodQuickActions(
    pod: KubePod,
    deleting: Boolean,
    onLogs: () -> Unit,
    onDelete: () -> Unit,
    onFlows: (() -> Unit)?,
    background: Color,
    content: @Composable () -> Unit,
) {
    val denial = rememberKubeDenial(KubeAction.DELETE_POD, pod.namespace)
    val target = ShareTarget.pod(pod.namespace, pod.name)
    val logs = RowAction(Icons.AutoMirrored.Outlined.Article, stringResource(R.string.row_logs), onLogs)
    val delete = RowAction(
        Icons.Outlined.Delete,
        stringResource(R.string.common_delete),
        onDelete,
        enabled = !deleting && pod.status != "Terminating" && denial == null,
        disabledReason = denial.denialReason(),
        destructive = true,
    )
    val flows = onFlows?.let { RowAction(Icons.Outlined.Stream, stringResource(R.string.flows_title), it) }
    val menu = listOfNotNull(logs, flows, rememberPinAction(target, pod.name), rememberShareAction(target), delete)
    QuickActionRow(leading = logs, trailing = delete, menu = menu, onClick = null) {
        Box(Modifier.background(background)) { content() }
    }
}

/**
 * A workload row's quick actions: swipe right to restart it (the confirmation first); a long
 * press lists Restart, Scale… (its sheet), Pin and Share. A tap still opens the sheet ([onOpen]).
 */
@Composable
internal fun WorkloadQuickActions(
    workload: KubeWorkload,
    restarting: Boolean,
    onRestart: () -> Unit,
    onOpen: () -> Unit,
    content: @Composable () -> Unit,
) {
    val denial = rememberKubeDenial(KubeAction.restart(workload.kind), workload.namespace)
    val target = ShareTarget.workload(workload.kind, workload.namespace, workload.name)
    val restart = RowAction(
        Icons.Outlined.RestartAlt,
        stringResource(R.string.workloads_restart_confirm),
        onRestart,
        enabled = !restarting && workload.canRestart && denial == null,
        disabledReason = denial.denialReason(),
    )
    val scale = RowAction(Icons.Outlined.OpenInFull, stringResource(R.string.row_scale), onOpen).takeIf { workload.canScale }
    val menu = listOfNotNull(restart, scale, rememberPinAction(target, workload.name), rememberShareAction(target))
    QuickActionRow(
        leading = restart,
        trailing = null,
        menu = menu,
        onClick = onOpen,
        onClickLabel = stringResource(R.string.common_open),
    ) {
        Box(Modifier.background(CardDefaults.cardColors().containerColor)) { content() }
    }
}

/**
 * A CronJob card's quick actions: swipe right to run it now, left to suspend or resume it, each
 * through its confirmation ([onRun], [onSuspend]: the Argo CD freeze-first path stays there);
 * a long press lists the two, Pin and Share. A tap still expands its runs ([onToggle]).
 */
@Composable
internal fun CronJobQuickActions(
    cronJob: KubeCronJob,
    triggering: Boolean,
    suspending: Boolean,
    runDenial: KubePermission?,
    suspendDenial: KubePermission?,
    onToggle: () -> Unit,
    onRun: () -> Unit,
    onSuspend: () -> Unit,
    content: @Composable () -> Unit,
) {
    val target = ShareTarget.cronJob(cronJob.namespace, cronJob.name)
    val run = RowAction(
        Icons.Outlined.PlayArrow,
        stringResource(R.string.cronjobs_run_now),
        onRun,
        enabled = !triggering && cronJob.triggerable && runDenial == null,
        disabledReason = runDenial.denialReason() ?: stringResource(R.string.cronjobs_schedule_only).takeIf { !cronJob.triggerable },
    )
    val suspend = RowAction(
        if (cronJob.suspended) Icons.Outlined.PlayCircle else Icons.Outlined.PauseCircle,
        stringResource(if (cronJob.suspended) R.string.cronjobs_resume else R.string.cronjobs_suspend),
        onSuspend,
        enabled = !suspending && suspendDenial == null,
        disabledReason = suspendDenial.denialReason(),
    )
    val menu = listOfNotNull(run, suspend, rememberPinAction(target, cronJob.name), rememberShareAction(target))
    val shape = CardDefaults.shape
    QuickActionRow(
        leading = run,
        trailing = suspend,
        menu = menu,
        onClick = onToggle,
        modifier = Modifier.clip(shape),
        shape = shape,
        content = content,
    )
}
