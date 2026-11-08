package name.levis.ichor.data

import android.content.Context
import name.levis.ichor.model.OnCallConfig

/**
 * Each cluster's on-call integration, token included, in its own sealed file (see
 * [ClusterSecureFiles]): only on this device, never in a backup.
 */
class OnCallStore(context: Context) {
    private val files = ClusterSecureFiles(context, "oncall")

    /** The settings of the cluster; unset when there are none or they cannot be opened any more. */
    fun read(fingerprint: String): OnCallConfig = try {
        files.read(fingerprint)?.let { TalosJson.decodeFromString(OnCallConfig.serializer(), it) } ?: OnCallConfig()
    } catch (_: Exception) {
        OnCallConfig()
    }

    fun save(fingerprint: String, config: OnCallConfig) =
        files.write(fingerprint, TalosJson.encodeToString(OnCallConfig.serializer(), config))

    /** Forgets the settings of the clusters not in [fingerprints]. */
    fun sync(fingerprints: Collection<String>) = files.sync(fingerprints)
}
