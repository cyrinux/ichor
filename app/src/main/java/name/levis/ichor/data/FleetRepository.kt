package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.FleetSummary
import name.levis.ichor.monitor.FleetEntries
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.monitor.demoFleetOnly
import name.levis.ichor.monitor.fleetEntries
import name.levis.ichorgo.Ichorgo

/** What the fleet screen is drawn from: the monitor's snapshots, the clusters, their names, screenshot mode and their tags. */
data class FleetSources(
    val state: MonitorState,
    val summary: ConfigSummary?,
    val names: Map<String, String>,
    val masked: Boolean,
    val tags: Map<String, List<String>> = emptyMap(),
)

/**
 * The fleet screen (every cluster's last background check, worst first), ranked by the Go core
 * from the snapshots the monitor kept. Never calls a cluster. With the demo alone on the phone,
 * the demo's made-up fleet.
 */
class FleetRepository(
    private val rank: (entries: String, now: Long) -> String = { entries, now -> Ichorgo.fleetSummary(entries, now) },
    private val demo: (now: Long) -> String = { now -> Ichorgo.demoFleetSummary(now) },
) {
    /** The fleet of [sources] as of [now] (unix ms). */
    suspend fun summary(sources: FleetSources, now: Long): FleetSummary = withContext(Dispatchers.Default) {
        val json = if (demoFleetOnly(sources.summary)) {
            demo(now)
        } else {
            val entries = fleetEntries(sources.state, sources.summary, sources.names, sources.masked, sources.tags)
            rank(FleetJson.encodeToString(FleetEntries.serializer(), FleetEntries(entries)), now)
        }
        TalosJson.decodeFromString(FleetSummary.serializer(), json)
    }

    private companion object {
        /** Defaults written out: the Go core reads a missing field as its zero value. */
        val FleetJson = Json(TalosJson) { encodeDefaults = true }
    }
}
