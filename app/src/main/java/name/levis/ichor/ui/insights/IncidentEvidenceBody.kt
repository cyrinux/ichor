package name.levis.ichor.ui.insights

import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.serialization.json.*
import name.levis.ichor.R
import name.levis.ichor.model.IncidentEntry
import name.levis.ichor.model.incidentDetail
import name.levis.ichor.model.incidentMetrics
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.live.BottleneckContent

/** A recorder entry's full evidence, shown when its timeline row is expanded: [timestamp] is absolute. */
@Composable
fun IncidentEvidenceBody(entry: IncidentEntry, timestamp: String) {
    val detail = remember(entry.detail) { entry.incidentDetail() }
    val metrics = remember(entry) { entry.incidentMetrics() }
    fun value(key: String) = (detail?.get(key) as? JsonPrimitive)?.contentOrNull.orEmpty()
    var expanded by rememberSaveable(entry.id) { mutableStateOf(false) }
    Column(Modifier.padding(top = 4.dp, bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        MutedText("$timestamp · ${entry.node}")
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
                Text("${value("state")} · ${serviceHealthLabel(value("health"))}")
                if (value("message").isNotBlank()) Text(value("message"))
            }
            "link" -> Text(stringResource(R.string.evidence_link_change, value("before"), value("after")))
            "recovered" -> Text(stringResource(R.string.evidence_available))
            else -> if (entry.detail.isNotBlank()) Text(entry.detail, maxLines = 4)
        }
        if (entry.detail.isNotBlank()) {
            TextButton(onClick = { expanded = !expanded }, contentPadding = PaddingValues(0.dp)) {
                Text(stringResource(if (expanded) R.string.evidence_hide_details else R.string.evidence_details))
            }
            if (expanded) Text(entry.detail, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        }
    }
}

@Composable
private fun serviceHealthLabel(health: String): String = stringResource(
    when (health) {
        "healthy" -> R.string.common_status_healthy
        "unhealthy" -> R.string.common_status_unhealthy
        else -> R.string.kubespan_peer_unknown
    },
)

/** A row's kind, the first part of its subtitle. */
@Composable
fun evidenceKindLabel(kind: String): String = stringResource(
    when (kind) {
        "status" -> R.string.insights_kind_status
        "service" -> R.string.insights_kind_service
        "link" -> R.string.insights_kind_link
        "metrics" -> R.string.insights_kind_metrics
        "error" -> R.string.insights_kind_error
        "recovered" -> R.string.insights_kind_recovered
        else -> R.string.insights_kind_event
    },
)

/** A row's state, the middle of its subtitle; empty when the kind has none worth a word. */
@Composable
fun evidenceStateLabel(entry: IncidentEntry): String {
    val detail = remember(entry.detail) { entry.incidentDetail() }
    fun value(key: String) = (detail?.get(key) as? JsonPrimitive)?.contentOrNull.orEmpty()
    fun flag(key: String) = (detail?.get(key) as? JsonPrimitive)?.booleanOrNull
    return when (entry.kind) {
        "status" -> stringResource(
            when {
                flag("reachable") == false -> R.string.common_status_unreachable
                flag("ready") == true && flag("reachable") == true -> R.string.common_status_ready
                flag("ready") == false -> R.string.common_status_not_ready
                else -> R.string.kubespan_peer_unknown
            },
        )
        "service" -> value("state").ifBlank { serviceHealthLabel(value("health")) }
        "link" -> value("after")
        "error" -> stringResource(R.string.evidence_unavailable)
        else -> ""
    }
}
