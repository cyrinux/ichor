package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * The imported plugins as a backup carries them (format 5): Ichorgo.pluginUserExport's answer
 * as is, each plugin's file and whether it is on. The iOS app writes and reads the same object.
 */
@Serializable
data class BackupPlugins(val plugins: List<BackupPlugin> = emptyList())

@Serializable
data class BackupPlugin(val id: String, val enabled: Boolean = false, val yaml: String = "")

/** Ichorgo.pluginUserRestore's answer: the ids stored, and the plugins left out with the reason. */
@Serializable
data class PluginRestoreResult(
    val restored: List<String> = emptyList(),
    val errors: List<PluginRestoreError> = emptyList(),
)

@Serializable
data class PluginRestoreError(val id: String, val error: String = "")

/** The plugins of a restored [payload]; null before format 5 or when it has none. */
fun restoredPlugins(payload: BackupPayload): BackupPlugins? =
    payload.plugins?.takeIf { payload.format >= BACKUP_FORMAT_PLUGINS && it.plugins.isNotEmpty() }

/** The skipped plugins as "id: reason" lines, for the restore's status. */
val PluginRestoreResult.skippedText: String
    get() = errors.joinToString("\n") { if (it.error.isEmpty()) it.id else "${it.id}: ${it.error}" }

/** What a scanned plugin code holds (Ichorgo.pluginQRPayload): a link to fetch, or the file itself. */
@Serializable
data class PluginQrPayload(val kind: String, val url: String = "", val yaml: String = "")

/** [PluginQrPayload.kind] of a link to a plugin file. */
const val PLUGIN_QR_URL = "url"
