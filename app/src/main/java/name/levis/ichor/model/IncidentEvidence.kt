package name.levis.ichor.model

import kotlinx.serialization.json.JsonObject
import name.levis.ichor.data.TalosJson

/** Legacy recordings may contain truncated raw JSON; that is unavailable evidence. */
fun IncidentEntry.incidentDetail(): JsonObject? = runCatching {
    TalosJson.parseToJsonElement(detail) as? JsonObject
}.getOrNull()

fun IncidentEntry.incidentMetrics(): Bottlenecks? = metrics ?: runCatching {
    val rates = incidentDetail()?.get("rates") ?: return@runCatching null
    TalosJson.decodeFromJsonElement(Bottlenecks.serializer(), rates)
}.getOrNull()
