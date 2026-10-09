package name.levis.ichor.monitor

import android.content.SharedPreferences
import java.security.MessageDigest

/**
 * The alerts snoozed from their notification: until when, per cluster (fingerprint) and alert
 * key. While snoozed, the monitor posts nothing for that key, problem or resolved. Keys name
 * nodes and apps: stored hashed, only the end time in clear. Expired entries are pruned, and so
 * are a removed cluster's.
 */
class AlertSnoozes(private val prefs: SharedPreferences) {

    fun snooze(cluster: String, key: String, until: Long) {
        if (cluster.isBlank() || key.isBlank()) return
        prefs.edit().putLong(entry(cluster, key), until).apply()
    }

    fun isSnoozed(cluster: String, key: String, now: Long): Boolean =
        prefs.getLong(entry(cluster, key), 0L) > now

    /** [alerts] less those snoozed on [cluster] at [now]. */
    fun unsnoozed(alerts: List<Alert>, cluster: String, now: Long): List<Alert> =
        alerts.filterNot { isSnoozed(cluster, it.key, now) }

    /** Forgets the snoozes over by [now]. */
    fun prune(now: Long) {
        val over = prefs.all.filter { (key, value) -> key.startsWith(PREFIX) && (value as? Long ?: 0L) <= now }.keys
        if (over.isEmpty()) return
        prefs.edit().apply { over.forEach { remove(it) } }.apply()
    }

    /** Forgets the snoozes of the clusters not among [clusters] (fingerprints): a removed cluster's go with it. */
    fun retain(clusters: Collection<String>) {
        val kept = clusters.map { PREFIX + hash(it) + ":" }
        val gone = prefs.all.keys.filter { key -> key.startsWith(PREFIX) && kept.none { key.startsWith(it) } }
        if (gone.isEmpty()) return
        prefs.edit().apply { gone.forEach { remove(it) } }.apply()
    }

    // The cluster's hash first, so its snoozes can be told apart without their keys.
    private fun entry(cluster: String, key: String): String = PREFIX + hash(cluster) + ":" + hash("$cluster|$key")

    private fun hash(text: String): String =
        MessageDigest.getInstance("SHA-256").digest(text.toByteArray()).joinToString("") { "%02x".format(it) }

    companion object {
        const val FILE = "ichor-alert-snoozes"
        private const val PREFIX = "snooze:"
    }
}
