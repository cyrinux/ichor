package name.levis.ichor.monitor

import android.content.SharedPreferences
import java.security.MessageDigest

/** What the alert rules did with an alert at [at] (unix ms): [reason] quiet or severity, [until] the quiet window's end. */
data class AlertMute(val reason: String, val until: Long, val at: Long)

/**
 * The alerts the rules kept quiet (quiet hours, minimum severity), per cluster (fingerprint) and
 * alert key, so the history can say why the phone stayed silent. The latest one per key; keys
 * name nodes and apps: stored hashed, like [AlertSnoozes]. Kept [MUTE_KEEP_MILLIS] (the longest
 * history window), and a removed cluster's go with it.
 */
class AlertMutes(private val prefs: SharedPreferences) {

    fun record(cluster: String, key: String, mute: AlertMute) {
        if (cluster.isBlank() || key.isBlank()) return
        prefs.edit().putString(entry(cluster, key), "${mute.reason}|${mute.until}|${mute.at}").apply()
    }

    /** The latest mute of [key] on [cluster], or null. */
    fun of(cluster: String, key: String): AlertMute? {
        if (cluster.isBlank() || key.isBlank()) return null
        return parse(prefs.getString(entry(cluster, key), null))
    }

    /**
     * The mute of [key] on [cluster] while it was open, from [from] to [to] (null: still open):
     * what the history line of that alert says.
     */
    fun during(cluster: String, key: String, from: Long, to: Long?): AlertMute? =
        of(cluster, key)?.takeIf { it.at >= from && (to == null || it.at <= to) }

    /** Forgets the mutes older than [MUTE_KEEP_MILLIS] at [now]. */
    fun prune(now: Long) {
        val old = prefs.all.filter { (key, value) ->
            key.startsWith(PREFIX) && (parse(value as? String)?.at ?: 0L) < now - MUTE_KEEP_MILLIS
        }.keys
        if (old.isEmpty()) return
        prefs.edit().apply { old.forEach { remove(it) } }.apply()
    }

    /** Forgets the mutes of the clusters not among [clusters] (fingerprints). */
    fun retain(clusters: Collection<String>) {
        val kept = clusters.map { PREFIX + hash(it) + ":" }
        val gone = prefs.all.keys.filter { key -> key.startsWith(PREFIX) && kept.none { key.startsWith(it) } }
        if (gone.isEmpty()) return
        prefs.edit().apply { gone.forEach { remove(it) } }.apply()
    }

    private fun entry(cluster: String, key: String): String = PREFIX + hash(cluster) + ":" + hash("$cluster|$key")

    private fun hash(text: String): String =
        MessageDigest.getInstance("SHA-256").digest(text.toByteArray()).joinToString("") { "%02x".format(it) }

    private fun parse(value: String?): AlertMute? {
        val parts = value?.split('|') ?: return null
        if (parts.size != 3) return null
        val until = parts[1].toLongOrNull() ?: return null
        val at = parts[2].toLongOrNull() ?: return null
        return AlertMute(parts[0], until, at)
    }

    companion object {
        const val FILE = "ichor-alert-mutes"
        private const val PREFIX = "mute:"

        /** 30 days, the longest history window. */
        const val MUTE_KEEP_MILLIS = 30L * 86_400_000L
    }
}
