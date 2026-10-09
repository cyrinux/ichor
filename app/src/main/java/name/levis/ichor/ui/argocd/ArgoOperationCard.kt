package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.StopCircle
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoOperation
import name.levis.ichor.model.shortRevision
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The running or last sync: phase, message, how long it ran, "5 / 9 resources" with a bar, the
 * resources that failed with Argo CD's message, and Terminate while it runs.
 */
@Composable
fun ArgoOperationCard(app: ArgoApp, op: ArgoOperation, busy: Boolean, onTerminate: () -> Unit) {
    val colors = LocalStatusColors.current
    val (icon, color) = when {
        op.isRunning -> Icons.Outlined.Sync to MaterialTheme.colorScheme.primary
        op.isFailed -> Icons.Outlined.ErrorOutline to colors.bad
        else -> Icons.Outlined.CheckCircle to colors.ok
    }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(22.dp))
                Column(Modifier.weight(1f).padding(start = 10.dp)) {
                    Text(stringResource(R.string.argo_operation, op.phase), style = MaterialTheme.typography.titleMedium, color = color)
                    MutedText(subtitle(op))
                    if (op.revisionUrl.isNotEmpty()) {
                        RevisionText(op.revision, op.revisionUrl, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                Text(elapsed(op), style = MaterialTheme.typography.labelLarge, fontFamily = FontFamily.Monospace)
            }
            if (op.message.isNotEmpty()) Text(op.message, style = MaterialTheme.typography.bodySmall)
            if (op.total > 0) Progress(app, op, color)
            op.failed.forEach { f ->
                Column(Modifier.padding(top = 2.dp)) {
                    Text("${f.kind}/${f.name}", style = MaterialTheme.typography.labelLarge, fontFamily = FontFamily.Monospace, color = colors.bad)
                    if (f.message.isNotEmpty()) MutedText(f.message)
                }
            }
            if (op.phase == "Running") {
                OutlinedButton(onClick = onTerminate, enabled = !busy) {
                    Icon(Icons.Outlined.StopCircle, contentDescription = null, tint = colors.bad, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.argo_terminate), color = colors.bad, modifier = Modifier.padding(start = 6.dp))
                }
            }
        }
    }
}

@Composable
private fun Progress(app: ArgoApp, op: ArgoOperation, color: Color) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        LinearProgressIndicator(progress = { op.progress }, color = color, strokeCap = StrokeCap.Round, modifier = Modifier.fillMaxWidth())
        Row {
            Text(stringResource(R.string.argo_resources_progress, op.done, op.total), style = MaterialTheme.typography.labelMedium, modifier = Modifier.weight(1f))
            if (op.isRunning && op.waves.size > 1) {
                Text(
                    stringResource(R.string.argo_wave_of, op.wave, op.waveIndex, op.waves.size),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                )
            } else if (op.isRunning) {
                Text(waveProgress(app), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
            }
        }
    }
}

/** "by automated · 8.6.0 · retry 5 · dry run". */
@Composable
private fun subtitle(op: ArgoOperation): String = listOfNotNull(
    op.initiatedBy.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.argo_by, it) },
    // With a link, the revision has its own row under the subtitle.
    op.revision.takeIf { it.isNotEmpty() && op.revisionUrl.isEmpty() }?.let(::shortRevision),
    op.retryCount.takeIf { it > 0 }?.let { stringResource(R.string.argo_retry, it) },
    stringResource(R.string.argo_dry_run).takeIf { op.dryRun },
).joinToString(" · ")

/** Running: ticks every second from the start; finished: how long it took. */
@Composable
private fun elapsed(op: ArgoOperation): String {
    if (op.startedAt <= 0) return ""
    val now by produceState(System.currentTimeMillis(), op.isRunning) {
        while (op.isRunning) {
            delay(1_000)
            value = System.currentTimeMillis()
        }
    }
    val end = if (op.isRunning || op.finishedAt <= 0) now else op.finishedAt
    return localizedDuration(((end - op.startedAt) / 1000).coerceAtLeast(0))
}
