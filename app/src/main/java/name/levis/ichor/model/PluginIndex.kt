package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** PluginIndexFetch: a published list of plugin files to pick from, and [url], the index read. */
@Serializable
data class PluginIndex(
    val url: String = "",
    val generatedAt: String = "",
    val plugins: List<PluginIndexItem> = emptyList(),
)

/**
 * A plugin file an index lists. [installed]: an imported plugin has its id. [builtin]: a built-in
 * has its id, so it cannot be imported. [icon] only names a bundled icon, and the core drops a
 * [website] that is not https.
 */
@Serializable
data class PluginIndexItem(
    val id: String = "",
    val name: String = "",
    val description: String = "",
    val version: String = "",
    val url: String = "",
    val sha256: String = "",
    val resources: List<String> = emptyList(),
    val icon: String = "",
    val website: String = "",
    val maintainer: String = "",
    val installed: Boolean = false,
    val builtin: Boolean = false,
) {
    val title: String get() = name.ifEmpty { id }

    /** The tile: a bundled icon or a monogram, never a download. */
    val iconApp: InventoryApp get() = InventoryApp(id = id, name = title, icon = icon)
}
