package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.HistoryForecast
import name.levis.ichor.model.HistoryQuery
import name.levis.ichor.model.HistorySince
import name.levis.ichorgo.Ichorgo

/**
 * Reads each cluster's history ring ([HistoryStore]) through the Go core: what the screens show
 * (masked in screenshot mode like every other result) and the support bundle's anonymised copy.
 * Null when the cluster has no history yet or it cannot be read.
 */
class HistoryRepository(private val store: HistoryStore) {
    /** Node uptime, alerts, volume and memory series of the cluster [fingerprint] from [sinceMillis] to [nowMillis]. */
    suspend fun query(fingerprint: String, sinceMillis: Long, nowMillis: Long): HistoryQuery? = read(fingerprint) { ring ->
        TalosJson.decodeFromString(HistoryQuery.serializer(), Ichorgo.historyQuery(ring, sinceMillis, nowMillis))
    }

    /** What happened in the cluster [fingerprint] after [lastLookedMillis]. */
    suspend fun since(fingerprint: String, lastLookedMillis: Long): HistorySince? = read(fingerprint) { ring ->
        TalosJson.decodeFromString(HistorySince.serializer(), Ichorgo.historySince(ring, lastLookedMillis))
    }

    /**
     * Each volume's fill trend over the 7 days before [nowMillis], projected to [criticalPercent]
     * (the user's storage critical threshold) and to full.
     */
    suspend fun forecast(fingerprint: String, nowMillis: Long, criticalPercent: Int): HistoryForecast? = read(fingerprint) { ring ->
        TalosJson.decodeFromString(HistoryForecast.serializer(), Ichorgo.historyVolumeForecast(ring, nowMillis, criticalPercent.toDouble()))
    }

    /** The cluster [fingerprint]'s whole ring as anonymised JSON, for a support bundle. */
    suspend fun export(fingerprint: String): String? = read(fingerprint) { ring -> Ichorgo.historyExport(ring) }

    private suspend fun <T> read(fingerprint: String, block: (ByteArray) -> T): T? = withContext(Dispatchers.IO) {
        if (fingerprint.isBlank()) return@withContext null
        val ring = store.ring(fingerprint)?.takeIf { it.isNotEmpty() } ?: return@withContext null
        runCatching { block(ring) }.getOrNull()
    }
}
