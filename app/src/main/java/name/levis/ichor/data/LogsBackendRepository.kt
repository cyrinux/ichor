package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.LogsDiscovery
import name.levis.ichor.model.LogsResult
import name.levis.ichor.model.LogsValues
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.toGoJson
import name.levis.ichorgo.Ichorgo

/**
 * The cluster's log backend (Loki, VictoriaLogs), reached like a metrics source ([PromSource] of
 * that kind): its labels and searches. Results stay in memory only, and only the discovery is cached.
 */
class LogsBackendRepository(go: GoCall) : GoRepository(go) {
    /** The log backends among the cluster's Services, the likeliest first. */
    suspend fun discover(): List<PromSource> = go.remember(DISCOVERY) {
        go.kube { cfg, ctx, server ->
            TalosJson.decodeFromString(LogsDiscovery.serializer(), Ichorgo.logsDiscover(cfg, ctx, server)).candidates.map { it.source }
        }
    }

    /** Checks [source] answers: cleaned up by Go, with its kind and its secret. Throws why not. */
    suspend fun probe(source: PromSource): PromSource = go.kube { cfg, ctx, server ->
        val checked = TalosJson.decodeFromString(PromSource.serializer(), Ichorgo.logsProbe(cfg, ctx, server, source.toGoJson()))
        // Go never returns the secret; without authentication there is none to keep.
        checked.copy(secret = if (checked.auth == PromSource.AUTH_NONE) "" else source.secret.trim())
    }

    /** The label names of [source], or the values of [label]. */
    suspend fun labels(source: PromSource, label: String = ""): List<String> = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(LogsValues.serializer(), Ichorgo.logsLabels(cfg, ctx, server, source.toGoJson(), label)).values
    }

    /** Runs [query] on [source] between two unix ms times, the newest lines first. */
    suspend fun query(source: PromSource, query: String, startMillis: Long, endMillis: Long): LogsResult = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(LogsResult.serializer(), Ichorgo.logsQuery(cfg, ctx, server, source.toGoJson(), query, startMillis, endMillis, 0))
    }

    /** The query selecting one pod in a backend of [kind]. */
    fun podQuery(kind: String, namespace: String, pod: String): String = Ichorgo.logsPodQuery(kind, namespace, pod)

    /** The query selecting the pods of one workload in a backend of [kind]. */
    fun workloadQuery(kind: String, namespace: String, workload: String): String = Ichorgo.logsWorkloadQuery(kind, namespace, workload)

    private companion object {
        const val DISCOVERY = "logs-backend|discovery"
    }
}

/**
 * The log backend of the cluster [fingerprint]: the one the user chose ([store]), else the
 * likeliest one found (not saved, so a later install is found too); null when none. A discovery
 * that fails (the role may not list Services) finds none.
 */
suspend fun LogsBackendRepository.sourceFor(store: LogsBackendStore, fingerprint: String): PromSource? {
    val chosen = withContext(Dispatchers.IO) { store.read(fingerprint) }.source
    return chosen ?: try {
        discover().firstOrNull()
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
        null
    }
}
