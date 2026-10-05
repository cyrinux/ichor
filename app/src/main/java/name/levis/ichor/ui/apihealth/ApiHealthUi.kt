package name.levis.ichor.ui.apihealth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.API_PRIORITY_FULL
import name.levis.ichor.model.ApiCount
import name.levis.ichor.model.ApiFlow
import name.levis.ichor.model.ApiHealthReport
import name.levis.ichor.model.ApiPriority
import name.levis.ichor.model.ApiQueued
import name.levis.ichor.model.ApiRequestRow
import name.levis.ichor.model.ApiStatus
import name.levis.ichor.model.failedChecks
import name.levis.ichor.model.formatMs
import name.levis.ichor.model.formatRate
import name.levis.ichor.model.ratesAreLive
import name.levis.ichor.model.share
import name.levis.ichor.model.verdict
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors
import kotlin.math.roundToInt

@Composable
private fun ApiStatus.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        ApiStatus.OK -> colors.ok
        ApiStatus.BUSY -> colors.warn
        ApiStatus.THROTTLING, ApiStatus.UNHEALTHY -> colors.bad
    }
}

@Composable
private fun ApiStatus.label(): String = stringResource(
    when (this) {
        ApiStatus.OK -> R.string.apihealth_status_ok
        ApiStatus.BUSY -> R.string.apihealth_status_busy
        ApiStatus.THROTTLING -> R.string.apihealth_status_throttling
        ApiStatus.UNHEALTHY -> R.string.apihealth_status_unhealthy
    },
)

@Composable
private fun ApiStatus.hint(): String = stringResource(
    when (this) {
        ApiStatus.OK -> R.string.apihealth_status_ok_hint
        ApiStatus.BUSY -> R.string.apihealth_status_busy_hint
        ApiStatus.THROTTLING -> R.string.apihealth_status_throttling_hint
        ApiStatus.UNHEALTHY -> R.string.apihealth_status_unhealthy_hint
    },
)

/** The verdict and why, the version and uptime, what the rates cover, and failing checks. */
@Composable
fun VerdictHeader(r: ApiHealthReport) {
    val status = r.verdict
    val colors = LocalStatusColors.current
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            StatusPill(status.label(), status.color())
            if (r.version.isNotEmpty() && r.uptimeSeconds > 0) {
                MutedText(stringResource(R.string.apihealth_version_uptime, r.version, localizedDuration(r.uptimeSeconds)))
            } else if (r.version.isNotEmpty()) {
                MutedText(r.version)
            }
        }
        Text(status.hint(), style = MaterialTheme.typography.bodyMedium)
        if (r.metricsError.isEmpty()) {
            MutedText(
                if (r.ratesAreLive) {
                    stringResource(R.string.apihealth_window, r.windowSeconds.roundToInt())
                } else {
                    stringResource(R.string.apihealth_since_start)
                },
            )
        }
        listOf("readyz" to r.ready, "livez" to r.live).forEach { (name, probe) ->
            if (probe.error.isNotEmpty()) InlineError(stringResource(R.string.apihealth_probe_error, name, probe.error))
        }
        val checks = r.ready.checks.size
        if (checks > 0) MutedText(stringResource(R.string.apihealth_checks_passed, r.ready.checks.count { it.ok }, checks))
        r.failedChecks.forEach { check ->
            Text(
                listOf(check.name, check.reason).filter { it.isNotEmpty() }.joinToString(": "),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = colors.bad,
            )
        }
    }
}

/** A flow schema: its share of the busiest one's rate as a bar, rejections and queues tagged. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ClientRow(flow: ApiFlow, top: Double) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
        NameAndValue(flow.name, formatRate(flow.rate), sub = flow.priority)
        if (top > 0) ShareBar((flow.rate / top).toFloat().coerceIn(0f, 1f))
        if (flow.rejectedRate > 0 || flow.queued > 0) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (flow.rejectedRate > 0) TagBadge(stringResource(R.string.apihealth_rejected_rate, formatRate(flow.rejectedRate)), colors.bad)
                if (flow.queued > 0) TagBadge(pluralStringResource(R.plurals.apihealth_queued_count, flow.queued, flow.queued), colors.warn)
                if (flow.waitMs > 0) TagBadge(stringResource(R.string.apihealth_wait, formatMs(flow.waitMs)), colors.warn)
            }
        }
    }
}

/** A priority level's seats in use out of its limit; exempt levels have none. */
@Composable
fun PriorityRow(p: ApiPriority) {
    val colors = LocalStatusColors.current
    val seats = p.executing.roundToInt()
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
        val value = p.share?.let { stringResource(R.string.apihealth_seats, seats, p.limit.roundToInt()) }
            ?: stringResource(R.string.apihealth_seats_unlimited, seats)
        NameAndValue(p.name, value)
        p.share?.let { UsageBar(it, warnAt = API_PRIORITY_FULL) }
        if (p.queued > 0 || p.rejectedRate > 0) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (p.queued > 0) TagBadge(pluralStringResource(R.plurals.apihealth_queued_count, p.queued, p.queued), colors.warn)
                if (p.rejectedRate > 0) TagBadge(stringResource(R.string.apihealth_rejected_rate, formatRate(p.rejectedRate)), colors.bad)
            }
        }
    }
}

/** A verb on a resource, its rate, mean latency and server errors. */
@Composable
fun RequestRow(row: ApiRequestRow) {
    val colors = LocalStatusColors.current
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        TagBadge(row.verb, MaterialTheme.colorScheme.primary, mono = true)
        Column(Modifier.weight(1f)) {
            Text(
                row.resource.ifEmpty { stringResource(R.string.apihealth_non_resource) },
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = if (row.resource.isEmpty()) null else FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            val details = listOfNotNull(
                row.latencyMs.takeIf { it > 0 }?.let { stringResource(R.string.apihealth_latency, formatMs(it)) },
            )
            if (details.isNotEmpty()) MutedText(details.joinToString("  ·  "))
        }
        if (row.errorRate > 0) TagBadge(stringResource(R.string.apihealth_error_rate, formatRate(row.errorRate)), colors.warn)
        Text(formatRate(row.rate), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
    }
}

/** A request waiting in a queue now: who sent it, and what. */
@Composable
fun QueuedRow(q: ApiQueued) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 5.dp)) {
        Text(q.user.ifEmpty { q.flowSchema }, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
        MutedText(listOf(q.verb, q.path).filter { it.isNotEmpty() }.joinToString(" "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        MutedText("${q.flowSchema} → ${q.priority}", maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** A client's share of the busiest one: neutral, a big share is not a fault by itself. */
@Composable
private fun ShareBar(fraction: Float) {
    LinearProgressIndicator(
        progress = { fraction },
        color = MaterialTheme.colorScheme.primary,
        trackColor = MaterialTheme.colorScheme.surfaceVariant,
        modifier = Modifier.fillMaxWidth().height(4.dp),
        drawStopIndicator = {},
    )
}

@Composable
fun CountRow(c: ApiCount) = NameAndValue(c.resource, c.count.toString())

@Composable
private fun NameAndValue(name: String, value: String, sub: String = "") {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Column(Modifier.weight(1f)) {
            Text(name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (sub.isNotEmpty()) MutedText(sub, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Text(value, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
    }
}
