package name.levis.ichor.data

import android.content.Context
import name.levis.ichor.model.MetricsConfig
import java.io.File

/**
 * Each cluster's metrics source and panels, by context fingerprint, in one Keystore-encrypted
 * file: the source may hold a token or password. Only on this device (not in backups).
 */
class MetricsStore(context: Context) {
    private val directory = File(context.noBackupFilesDir, "metrics").apply { mkdirs() }

    private fun store(fingerprint: String): SecureStore {
        require(FINGERPRINT.matches(fingerprint)) { "bad cluster fingerprint" }
        return SecureStore(File(directory, "$fingerprint.enc"), "ichor-metrics-$fingerprint")
    }

    fun read(fingerprint: String): MetricsConfig =
        store(fingerprint).read()?.let { TalosJson.decodeFromString(MetricsConfig.serializer(), it.toString(Charsets.UTF_8)) } ?: MetricsConfig()

    fun save(fingerprint: String, config: MetricsConfig) =
        store(fingerprint).write(TalosJson.encodeToString(MetricsConfig.serializer(), config).toByteArray())

    /** Forgets the setups of the clusters not in [fingerprints]. */
    fun sync(fingerprints: Collection<String>) {
        directory.listFiles()?.forEach { file ->
            val fingerprint = file.name.removeSuffix(".enc")
            if (file.name.endsWith(".enc") && fingerprint !in fingerprints && FINGERPRINT.matches(fingerprint)) store(fingerprint).clear()
        }
    }

    private companion object {
        val FINGERPRINT = Regex("[A-Za-z0-9_-]{1,128}")
    }
}
