package name.levis.ichor.ui.argocd

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Restore
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoHistory
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.shortRevision
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/** A pod that is not ready next to the app: name, status, node, and a warning when Talos says the node is down. */
@Composable
fun UnhealthyPodRow(pod: KubePod, nodeDown: Boolean) {
    val colors = LocalStatusColors.current
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(8.dp).background(if (pod.transitional) colors.warn else colors.bad, CircleShape))
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(pod.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOf(pod.status, pod.node.ifEmpty { stringResource(R.string.cnpg_pod_pending) }).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (nodeDown) {
            Icon(Icons.Outlined.WarningAmber, contentDescription = null, tint = colors.bad, modifier = Modifier.size(16.dp))
            Text(
                stringResource(R.string.argo_cause_node, pod.node),
                style = MaterialTheme.typography.labelSmall,
                color = colors.bad,
                modifier = Modifier.padding(start = 4.dp),
            )
        }
    }
}

/**
 * One deployment of the history, as a timeline entry: revision, chart or target revision, when
 * and by whom; [onRollback] (null when not allowed or for the current one) offers going back to it.
 */
@Composable
fun HistoryRow(entry: ArgoHistory, current: Boolean, last: Boolean, onRollback: (() -> Unit)?) {
    val accent = if (current) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outline
    Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min)) {
        Column(Modifier.width(26.dp).fillMaxHeight(), horizontalAlignment = Alignment.CenterHorizontally) {
            Box(Modifier.padding(top = 6.dp).size(10.dp).background(accent, CircleShape))
            if (!last) Box(Modifier.padding(top = 2.dp).width(2.dp).weight(1f).background(MaterialTheme.colorScheme.outlineVariant))
        }
        Column(Modifier.weight(1f).padding(start = 8.dp, bottom = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    shortRevision(entry.revision),
                    style = MaterialTheme.typography.bodyMedium,
                    fontFamily = FontFamily.Monospace,
                    color = if (current) accent else MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                if (current) Text(stringResource(R.string.argo_history_current), style = MaterialTheme.typography.labelSmall, color = accent)
            }
            Text(
                listOf(
                    if (entry.chart.isNotEmpty()) "${entry.chart}@${entry.targetRevision}" else entry.targetRevision,
                    timeAgo(entry.deployedAt),
                    entry.initiatedBy.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.argo_by, it) }.orEmpty(),
                ).filter { it.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (onRollback != null) {
                TextButton(onClick = onRollback, modifier = Modifier.padding(start = 0.dp)) {
                    Icon(Icons.Outlined.Restore, contentDescription = null, modifier = Modifier.size(16.dp))
                    Text(stringResource(R.string.argo_rollback), modifier = Modifier.padding(start = 6.dp))
                }
            }
        }
    }
}
