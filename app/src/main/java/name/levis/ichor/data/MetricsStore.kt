package name.levis.ichor.data

import android.content.Context
import name.levis.ichor.model.AlertmanagerConfig
import name.levis.ichor.model.MetricsConfig
import java.io.File

/**
 * One Keystore-encrypted file per cluster, by context fingerprint, in the directory [name]: a
 * source kept there may hold a token or password. Only on this device (not in backups).
 */
class ClusterSecureFiles(context: Context, private val name: String) {
    private val directory = File(context.noBackupFilesDir, name).apply { mkdirs() }

    private fun store(fingerprint: String): SecureStore {
        require(FINGERPRINT.matches(fingerprint)) { "bad cluster fingerprint" }
        return SecureStore(File(directory, "$fingerprint.enc"), "ichor-$name-$fingerprint")
    }

    fun read(fingerprint: String): String? = store(fingerprint).read()?.toString(Charsets.UTF_8)

    fun write(fingerprint: String, text: String) = store(fingerprint).write(text.toByteArray())

    /** Forgets the files of the clusters not in [fingerprints]. */
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

/** Each cluster's metrics source and panels (see [ClusterSecureFiles]). */
class MetricsStore(context: Context) {
    private val files = ClusterSecureFiles(context, "metrics")

    fun read(fingerprint: String): MetricsConfig =
        files.read(fingerprint)?.let { TalosJson.decodeFromString(MetricsConfig.serializer(), it) } ?: MetricsConfig()

    fun save(fingerprint: String, config: MetricsConfig) =
        files.write(fingerprint, TalosJson.encodeToString(MetricsConfig.serializer(), config))

    /** Forgets the setups of the clusters not in [fingerprints]. */
    fun sync(fingerprints: Collection<String>) = files.sync(fingerprints)
}

/**
 * Each cluster's Alertmanager source, kept apart from its metrics source (another Service),
 * see [ClusterSecureFiles].
 */
class AlertmanagerStore(context: Context) {
    private val files = ClusterSecureFiles(context, "alertmanager")

    fun read(fingerprint: String): AlertmanagerConfig =
        files.read(fingerprint)?.let { TalosJson.decodeFromString(AlertmanagerConfig.serializer(), it) } ?: AlertmanagerConfig()

    fun save(fingerprint: String, config: AlertmanagerConfig) =
        files.write(fingerprint, TalosJson.encodeToString(AlertmanagerConfig.serializer(), config))

    /** Forgets the sources of the clusters not in [fingerprints]. */
    fun sync(fingerprints: Collection<String>) = files.sync(fingerprints)
}
