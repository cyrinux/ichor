package name.levis.ichor.monitor

import android.content.SharedPreferences
import java.security.MessageDigest

/**
 * The alerts acknowledged from their notification, per cluster (fingerprint) and alert key: an
 * acknowledged alert is not posted again by its repeat (the notification and its history stay).
 * Keys name nodes and apps: stored hashed, like [AlertSnoozes]. An acknowledgement ends when the
 * alert stops firing (see [retainFiring]), and a removed cluster's go with it.
 */
class AlertAcks(private val prefs: SharedPreferences) {

    fun acknowledge(cluster: String, key: String, at: Long) {
        if (cluster.isBlank() || key.isBlank()) return
        prefs.edit().putLong(entry(cluster, key), at).apply()
    }

    fun isAcknowledged(cluster: String, key: String): Boolean =
        cluster.isNotBlank() && key.isNotBlank() && prefs.contains(entry(cluster, key))

    /** Forgets the acknowledgements of [cluster] whose alert is not among [firing] (keys): it recovered. */
    fun retainFiring(cluster: String, firing: Collection<String>) {
        val prefix = PREFIX + hash(cluster) + ":"
        val kept = firing.map { entry(cluster, it) }.toSet()
        val gone = prefs.all.keys.filter { it.startsWith(prefix) && it !in kept }
        if (gone.isEmpty()) return
        prefs.edit().apply { gone.forEach { remove(it) } }.apply()
    }

    /** Forgets the acknowledgements of the clusters not among [clusters] (fingerprints). */
    fun retain(clusters: Collection<String>) {
        val kept = clusters.map { PREFIX + hash(it) + ":" }
        val gone = prefs.all.keys.filter { key -> key.startsWith(PREFIX) && kept.none { key.startsWith(it) } }
        if (gone.isEmpty()) return
        prefs.edit().apply { gone.forEach { remove(it) } }.apply()
    }

    // The cluster's hash first, so its acknowledgements can be told apart without their keys.
    private fun entry(cluster: String, key: String): String = PREFIX + hash(cluster) + ":" + hash("$cluster|$key")

    private fun hash(text: String): String =
        MessageDigest.getInstance("SHA-256").digest(text.toByteArray()).joinToString("") { "%02x".format(it) }

    companion object {
        const val FILE = "ichor-alert-acks"
        private const val PREFIX = "ack:"
    }
}
