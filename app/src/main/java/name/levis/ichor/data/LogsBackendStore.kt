package name.levis.ichor.data

import android.content.Context
import name.levis.ichor.model.LogsBackendConfig

/**
 * Each cluster's log backend and saved queries, see [ClusterSecureFiles]: the source may hold a
 * token, and a saved query names the cluster's namespaces and pods.
 */
class LogsBackendStore(context: Context) {
    private val files = ClusterSecureFiles(context, "logs-backend")

    fun read(fingerprint: String): LogsBackendConfig =
        files.read(fingerprint)?.let { TalosJson.decodeFromString(LogsBackendConfig.serializer(), it) } ?: LogsBackendConfig()

    fun save(fingerprint: String, config: LogsBackendConfig) =
        files.write(fingerprint, TalosJson.encodeToString(LogsBackendConfig.serializer(), config))

    /** Forgets the setups of the clusters not in [fingerprints]. */
    fun sync(fingerprints: Collection<String>) = files.sync(fingerprints)
}
