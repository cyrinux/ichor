package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/plugin_findings.go (docs/plugins.md "Findings").

/**
 * KubePluginFindings: the findings of every plugin the cluster runs, for the background
 * monitor. [plugins] counts the active plugins read; [truncated] the findings left out past
 * the cap; [errors] the plugins (or their resources) that could not be read, whose other
 * findings still stand.
 */
@Serializable
data class PluginFindingsReport(
    val plugins: Int = 0,
    val findings: List<PluginFindingItem> = emptyList(),
    val truncated: Int = 0,
    val errors: List<PluginFindingsError> = emptyList(),
)

/**
 * One finding of a plugin, its words already translated by Go. [key] is stable from one read
 * to the next ("plugin:<plugin>:<finding>:<namespace>/<name>"): the monitor dedupes on it.
 * [checkupNative]: the checkup reports it from a section of its own (External Secrets).
 */
@Serializable
data class PluginFindingItem(
    val severity: String = "",
    val checkupNative: Boolean = false,
    val key: String = "",
    val plugin: String = "",
    val pluginName: String = "",
    val pluginIcon: String = "",
    val id: String = "",
    val title: String = "",
    val detail: String = "",
    val ref: PluginRef? = null,
    val opens: String = "",
)

/** A plugin that could not be read in full ([resource] set) or at all. */
@Serializable
data class PluginFindingsError(
    val plugin: String = "",
    val pluginName: String = "",
    val resource: String = "",
    val message: String = "",
)
