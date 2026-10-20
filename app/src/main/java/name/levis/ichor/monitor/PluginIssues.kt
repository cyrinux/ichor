package name.levis.ichor.monitor

import kotlinx.serialization.Serializable
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.PLUGIN_KEY_PREFIX
import name.levis.ichor.model.PluginFindingsReport

/**
 * What a plugin finding's notification says, kept as the value of its issue (its key is the
 * finding's): its severity, the plugin, and the plugin's own words (already translated by Go).
 */
@Serializable
data class PluginIssue(
    val severity: String = DATA_WARNING,
    val plugin: String = "",
    val pluginName: String = "",
    val title: String = "",
    val detail: String = "",
) {
    fun encode(): String = TalosJson.encodeToString(serializer(), this)

    companion object {
        /** A value this build cannot read is a warning of no plugin: it still clears once gone. */
        fun parse(value: String): PluginIssue = runCatching { TalosJson.decodeFromString(serializer(), value) }.getOrElse { PluginIssue() }
    }
}

/** A plugin issue's severity ([DATA_CRITICAL] or [DATA_WARNING]). */
fun pluginSeverity(value: String): String = PluginIssue.parse(value).severity

/**
 * The plugin findings worth a notification, keyed by their stable key ("plugin:<plugin>:<finding>:
 * <namespace>/<name>") with a [PluginIssue]: warnings and critical ones (a note is shown, not
 * notified). With [checkupWatched], the findings the checkup reports from a section of its own
 * (checkupNative, External Secrets) are left to it, or the same object alerts twice.
 */
fun pluginIssuesOf(report: PluginFindingsReport, checkupWatched: Boolean): Map<String, String> = buildMap {
    report.findings.forEach { f ->
        if (f.key.isEmpty() || (checkupWatched && f.checkupNative)) return@forEach
        val severity = when (f.severity) {
            DATA_CRITICAL -> DATA_CRITICAL
            DATA_WARNING -> DATA_WARNING
            else -> return@forEach
        }
        put(f.key, PluginIssue(severity, f.plugin, f.pluginName, f.title, f.detail).encode())
    }
}

/**
 * [pluginIssuesOf] for a read that left some out: a plugin that could not be read (in full or in
 * part) keeps the issues [known] had for it, and a truncated read keeps every known one past its
 * cut, so they neither clear falsely nor come back as new once it reads again.
 */
fun pluginIssuesWithGaps(report: PluginFindingsReport, known: Map<String, String>, checkupWatched: Boolean): Map<String, String> {
    val fresh = pluginIssuesOf(report, checkupWatched)
    val unread = report.errors.map { it.plugin }.toSet()
    val kept = known.filterKeys { key ->
        key !in fresh && (report.truncated > 0 || pluginOfKey(key) in unread)
    }
    return kept + fresh
}

/** The plugin id of a plugin finding's key ("plugin:<plugin>:…"). */
fun pluginOfKey(key: String): String = key.removePrefix(PLUGIN_KEY_PREFIX).substringBefore(':')

/**
 * The plugin issues a partial read may carry over: [previous]'s, only when it is the same cluster
 * ([context]) and that check watched and read them.
 */
fun knownPluginIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.pluginWatched && it.pluginChecked }?.pluginIssues.orEmpty()
