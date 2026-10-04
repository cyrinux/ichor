package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.PublicIpReport
import name.levis.ichorgo.Ichorgo

/**
 * Public IPs Talos does not know, asked from the internet through the Kubernetes API
 * (os:admin): one curl pod per node, in a temporary namespace. The last answer of each
 * cluster is kept, by fingerprint, only on this device and encrypted ([sealed]: the
 * addresses locate the cluster); forgotten with the cluster.
 */
class PublicIpRepository(
    private val configs: ConfigRepository,
    private val kubeServers: KubeServers,
    private val sealed: SealedValue,
    /** Whether the core masks addresses now (screenshot mode): a report it returned then is not kept. */
    private val masked: () -> Boolean,
) {
    private val serializer = MapSerializer(String.serializer(), PublicIpReport.serializer())

    /** Serializes the read-modify-write of [_reports] and of the file behind it. */
    private val lock = Any()

    private val _reports = MutableStateFlow(
        sealed.read()?.let { runCatching { TalosJson.decodeFromString(serializer, it) }.getOrNull() }.orEmpty(),
    )
    val reports: StateFlow<Map<String, PublicIpReport>> = _reports.asStateFlow()

    private val _running = MutableStateFlow(emptySet<String>())

    /** The clusters (fingerprints) being probed. */
    val running: StateFlow<Set<String>> = _running.asStateFlow()

    /**
     * Probes the active cluster's nodes and keeps the answer; it takes up to a few minutes.
     * One returned while screenshot mode is on holds placeholders, not addresses: not kept.
     */
    suspend fun detect(): PublicIpReport = withContext(Dispatchers.IO) {
        val stored = configs.forCall()
        val fingerprint = stored.activeSummary?.fingerprint.orEmpty()
        val server = kubeServers.servers.value[fingerprint].orEmpty()
        _running.update { it + fingerprint }
        try {
            val json = Ichorgo.detectPublicIPs(stored.yaml, stored.activeContext, server)
            TalosJson.decodeFromString(PublicIpReport.serializer(), json).also { report ->
                if (fingerprint.isNotBlank() && !masked()) store { it + (fingerprint to report) }
            }
        } finally {
            _running.update { it - fingerprint }
        }
    }

    /** Forgets the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) {
        val fingerprints = summary.contexts.map { it.fingerprint }.toSet()
        store { reports -> reports.filterKeys { it in fingerprints } }
    }

    private fun store(change: (Map<String, PublicIpReport>) -> Map<String, PublicIpReport>) = synchronized(lock) {
        val reports = change(_reports.value)
        if (reports == _reports.value) return@synchronized
        _reports.value = reports
        if (reports.isEmpty()) sealed.delete() else sealed.write(TalosJson.encodeToString(serializer, reports))
    }
}
