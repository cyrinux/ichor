package name.levis.ichor.ui.insights

import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ShowChart
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.serialization.json.*
import name.levis.ichor.R
import name.levis.ichor.model.IncidentEntry
import name.levis.ichor.model.incidentDetail
import name.levis.ichor.model.incidentMetrics
import name.levis.ichor.ui.live.BottleneckContent
import name.levis.ichor.ui.theme.LocalStatusColors

@Composable
fun IncidentEvidenceCard(entry: IncidentEntry, timestamp: String) {
    val detail = remember(entry.detail) { entry.incidentDetail() }
    val metrics = remember(entry) { entry.incidentMetrics() }
    fun value(key: String) = (detail?.get(key) as? JsonPrimitive)?.contentOrNull.orEmpty()
    val reachable = (detail?.get("reachable") as? JsonPrimitive)?.booleanOrNull
    val ready = (detail?.get("ready") as? JsonPrimitive)?.booleanOrNull
    val status = when {
        reachable == false -> R.string.common_status_unreachable
        ready == true && reachable == true -> R.string.common_status_ready
        ready == false -> R.string.common_status_not_ready
        else -> R.string.kubespan_peer_unknown
    }
    val title = stringResource(when (entry.kind) {
        "status" -> status
        "metrics" -> R.string.insights_bottlenecks
        "service" -> R.string.evidence_service
        "link" -> R.string.evidence_link
        "error" -> R.string.evidence_unavailable
        "recovered" -> R.string.evidence_recovered
        else -> R.string.overview_action_events
    })
    val warning = entry.severity == "warning" || entry.severity == "error" ||
        (entry.kind == "status" && (reachable == false || ready == false)) ||
        (entry.kind == "service" && value("health") == "unhealthy") ||
        (metrics != null && (metrics.errors.isNotEmpty() || metrics.network.any { it.errors > 0 || it.drops > 0 }))
    val positive = !warning && (entry.kind == "recovered" || (entry.kind == "status" && ready == true && reachable == true) || (entry.kind == "service" && value("health") == "healthy"))
    val colors = LocalStatusColors.current
    val color = when { entry.severity == "error" -> colors.bad; warning -> colors.warn; positive -> colors.ok; else -> MaterialTheme.colorScheme.primary }
    val icon = when { warning -> Icons.Default.Warning; positive -> Icons.Default.CheckCircle; entry.kind == "metrics" -> Icons.AutoMirrored.Filled.ShowChart; entry.kind == "service" -> Icons.Default.Settings; entry.kind == "link" -> Icons.Default.Link; else -> Icons.Default.Info }
    var expanded by remember(entry.id) { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("$timestamp · ${entry.node}", style = MaterialTheme.typography.labelMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(icon, contentDescription = null, tint = color)
                Text(title, style = MaterialTheme.typography.titleMedium, color = color)
            }
            if (entry.subject.isNotBlank()) Text(entry.subject, style = MaterialTheme.typography.labelLarge)
            when (entry.kind) {
                "metrics" -> if (metrics != null) {
                    BottleneckContent(metrics)
                    if (entry.metricsOmitted > 0) Text(stringResource(R.string.evidence_omitted, entry.metricsOmitted), style = MaterialTheme.typography.bodySmall)
                } else Text(stringResource(R.string.evidence_legacy_metrics), style = MaterialTheme.typography.bodyMedium)
                "status" -> {
                    if (value("stage").isNotBlank()) Text(stringResource(R.string.evidence_stage, value("stage")))
                    if (value("error").isNotBlank()) Text(value("error"), color = MaterialTheme.colorScheme.error)
                    (detail?.get("conditions") as? JsonArray)?.forEach { condition ->
                        val obj = condition as? JsonObject
                        val name = (obj?.get("name") as? JsonPrimitive)?.contentOrNull.orEmpty()
                        val reason = (obj?.get("reason") as? JsonPrimitive)?.contentOrNull.orEmpty()
                        Text("$name: $reason")
                    }
                }
                "service" -> {
                    val health = when (value("health")) { "healthy" -> stringResource(R.string.common_status_healthy); "unhealthy" -> stringResource(R.string.common_status_unhealthy); else -> stringResource(R.string.kubespan_peer_unknown) }
                    Text("${value("state")} · $health")
                    if (value("message").isNotBlank()) Text(value("message"))
                }
                "link" -> Text(stringResource(R.string.evidence_link_change, value("before"), value("after")))
                "recovered" -> Text(stringResource(R.string.evidence_available))
                else -> if (entry.detail.isNotBlank()) Text(entry.detail, maxLines = 4)
            }
            if (entry.detail.isNotBlank()) {
                TextButton(onClick = { expanded = !expanded }) { Text(stringResource(if (expanded) R.string.evidence_hide_details else R.string.evidence_details)) }
                if (expanded) Text(entry.detail, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            }
        }
    }
}
