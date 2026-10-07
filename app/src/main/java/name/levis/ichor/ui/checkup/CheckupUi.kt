package name.levis.ichor.ui.checkup

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Error
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CheckupFinding
import name.levis.ichor.model.CheckupKind
import name.levis.ichor.model.CheckupNode
import name.levis.ichor.model.CheckupRelease
import name.levis.ichor.model.CheckupSeverity
import name.levis.ichor.model.CheckupVolume
import name.levis.ichor.model.inTrouble
import name.levis.ichor.model.level
import name.levis.ichor.model.subject
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatPercent
import java.util.Locale

/** How many rows of a measured list show before "Show all". */
private const val LIST_PREVIEW = 6

/** One problem: what it is about, what happens, Kubernetes' own words and what to do. */
@Composable
fun FindingCard(f: CheckupFinding, now: Long) {
    val colors = LocalStatusColors.current
    val (icon, tint) = when (f.level) {
        CheckupSeverity.CRITICAL -> Icons.Outlined.Error to colors.bad
        CheckupSeverity.WARNING -> Icons.Outlined.WarningAmber to colors.warn
        CheckupSeverity.INFO -> Icons.Outlined.Info to colors.muted
    }
    Surface(Modifier.fillMaxWidth(), shape = RoundedCornerShape(10.dp), color = tint.copy(alpha = 0.07f)) {
        Row(Modifier.padding(10.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(20.dp))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(
                    if (f.kind == CheckupKind.EVENT && f.extra.isNotEmpty()) "${f.extra} ${f.subject}" else f.subject,
                    style = MaterialTheme.typography.labelLarge,
                    fontFamily = FontFamily.Monospace,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(checkupTitle(f, now), style = MaterialTheme.typography.bodyMedium)
                if (f.node.isNotEmpty()) MutedText(stringResource(R.string.checkup_on_node, f.node), maxLines = 1)
                f.detail.takeIf { it.isNotEmpty() }?.let {
                    Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = colors.muted, maxLines = 6, overflow = TextOverflow.Ellipsis)
                }
                checkupFix(f.kind).takeIf { it != 0 }?.let { MutedText(stringResource(it)) }
            }
        }
    }
}

/** A list cut to its first rows, with a button for the rest. */
@Composable
private fun <T> MeasuredList(title: String, rows: List<T>, row: @Composable (T) -> Unit) {
    if (rows.isEmpty()) return
    var all by rememberSaveable { mutableStateOf(false) }
    HorizontalDivider(Modifier.padding(top = 4.dp))
    Text(title, style = MaterialTheme.typography.labelLarge)
    (if (all) rows else rows.take(LIST_PREVIEW)).forEach { row(it) }
    if (rows.size > LIST_PREVIEW) {
        TextButton(onClick = { all = !all }) {
            Text(if (all) stringResource(R.string.checkup_show_less) else stringResource(R.string.checkup_show_all, rows.size.toString()))
        }
    }
}

private fun cores(value: Double): String = String.format(Locale.getDefault(), "%.1f", value)

/** What the pods of each node request of what it offers: the scheduler's view, not live usage. */
@Composable
fun NodeRequests(nodes: List<CheckupNode>) {
    MeasuredList(stringResource(R.string.checkup_requests_title), nodes.sortedByDescending { maxOf(it.cpuPercent, it.memoryPercent) }) { n ->
        Column(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(n.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                MutedText(stringResource(R.string.checkup_pods_of, n.pods.toString(), n.podCapacity.toString()))
            }
            RequestBar(stringResource(R.string.checkup_cpu), n.cpuPercent, stringResource(R.string.checkup_cores_of, cores(n.cpuRequests), cores(n.cpuAllocatable)))
            RequestBar(
                stringResource(R.string.checkup_memory),
                n.memoryPercent,
                "${formatBytes(n.memoryRequests.toLong())} / ${formatBytes(n.memoryAllocatable.toLong())}",
            )
        }
    }
}

@Composable
private fun RequestBar(label: String, percent: Double, amount: String) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        MutedText(label, modifier = Modifier.weight(0.22f), maxLines = 1)
        UsageBar((percent / 100).toFloat().coerceIn(0f, 1f), Modifier.weight(0.4f), warnAt = 0.8f)
        MutedText("${formatPercent(percent, 0)}  $amount", modifier = Modifier.weight(0.38f), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Each node's roles, taints and labels: what keeps pods away from it or draws them to it. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun NodeTaints(nodes: List<CheckupNode>) {
    val colors = LocalStatusColors.current
    MeasuredList(stringResource(R.string.checkup_taints_title), nodes) { n ->
        var labels by rememberSaveable(n.name) { mutableStateOf(false) }
        Column(Modifier.fillMaxWidth().expandable(labels, enabled = n.labels.isNotEmpty()) { labels = !labels }.padding(vertical = 3.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(n.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                if (n.kubelet.isNotEmpty()) MutedText(n.kubelet)
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                n.roles.forEach { TagBadge(it, MaterialTheme.colorScheme.primary) }
                if (!n.ready) TagBadge(stringResource(R.string.common_status_not_ready), colors.bad)
                if (n.cordoned) TagBadge(stringResource(R.string.checkup_cordoned), colors.warn)
                n.taints.forEach { TagBadge(it, colors.muted, mono = true) }
                if (n.taints.isEmpty()) MutedText(stringResource(R.string.checkup_no_taint))
            }
            if (labels) {
                n.labels.forEach { Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = colors.muted) }
            } else if (n.labels.isNotEmpty()) {
                MutedText(stringResource(R.string.checkup_labels, n.labels.size.toString()))
            }
        }
    }
}

/** Every claim with its level, fullest first; the unmeasured ones say so. */
@Composable
fun VolumeLevels(volumes: List<CheckupVolume>) {
    MeasuredList(stringResource(R.string.checkup_volumes_title), volumes) { v ->
        Column(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("${v.namespace}/${v.name}", style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                MutedText(
                    if (v.measured) {
                        "${formatBytes(v.used.toLong())} / ${formatBytes(v.capacity.toLong())}"
                    } else {
                        formatBytes(v.capacity.toLong())
                    },
                )
            }
            if (v.measured) {
                UsageBar((v.usedPercent / 100).toFloat().coerceIn(0f, 1f), warnAt = 0.85f)
            } else {
                MutedText(if (v.phase == "Bound") stringResource(R.string.checkup_volume_unmeasured) else v.phase)
            }
        }
    }
}

/** The Helm releases at their latest revision, those in trouble first; tapping one opens it. */
@Composable
fun HelmReleases(releases: List<CheckupRelease>, now: Long, onOpenRelease: (namespace: String, name: String) -> Unit) {
    val colors = LocalStatusColors.current
    val open = stringResource(R.string.common_open)
    MeasuredList(stringResource(R.string.checkup_releases_title), releases) { r ->
        Row(
            Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = open) { onOpenRelease(r.namespace, r.name) }.padding(vertical = 3.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Column(Modifier.weight(1f)) {
                Text("${r.namespace}/${r.name}", style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                MutedText(stringResource(R.string.checkup_release_revision, r.revision.toString(), ageSince(r.updated, now)), maxLines = 1)
            }
            TagBadge(r.status, if (r.inTrouble) colors.warn else colors.ok)
        }
    }
}
