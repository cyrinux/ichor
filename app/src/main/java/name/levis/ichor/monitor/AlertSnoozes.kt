package name.levis.ichor.monitor

import android.content.SharedPreferences
import java.security.MessageDigest

/**
 * The alerts snoozed from their notification: until when, per cluster (fingerprint) and alert
 * key. While snoozed, the monitor posts nothing for that key, problem or resolved. Keys name
 * nodes and apps: stored hashed, only the end time in clear. Expired entries are pruned.
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

    private fun entry(cluster: String, key: String): String {
        val digest = MessageDigest.getInstance("SHA-256").digest("$cluster|$key".toByteArray())
        return PREFIX + digest.joinToString("") { "%02x".format(it) }
    }

    companion object {
        const val FILE = "ichor-alert-snoozes"
        private const val PREFIX = "snooze:"
    }
}
