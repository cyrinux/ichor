package name.levis.ichor.model

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

// Mirrors go/ichorgo/talos_form.go (talosForm): a talosconfig context entered by hand, for
// BuildTalosconfig, then imported like a pasted talosconfig.

/** How the cluster is reached: directly with a client certificate, or through Omni. */
enum class TalosFormMode(val id: String) { DIRECT("direct"), OMNI("omni") }

/**
 * The "Enter details" form. [endpoints] and [nodes] are as typed: one per line (commas and
 * spaces separate too). [identity] is who an Omni context signs in as: the account email, or a
 * service account's identity; its key or a browser sign-in is asked once the cluster is added.
 */
data class TalosForm(
    val mode: TalosFormMode = TalosFormMode.DIRECT,
    val name: String = "",
    val endpoints: String = "",
    val nodes: String = "",
    val ca: String = "",
    val crt: String = "",
    val key: String = "",
    val omniUrl: String = "",
    val cluster: String = "",
    val identity: String = "",
) {
    val endpointList: List<String> get() = splitEntries(endpoints)
    val nodeList: List<String> get() = splitEntries(nodes)

    /** Every field the mode needs is filled; the core checks the rest (addresses, PEM). */
    val canSubmit: Boolean
        get() = name.isNotBlank() && when (mode) {
            TalosFormMode.DIRECT -> endpointList.isNotEmpty() && ca.isNotBlank() && crt.isNotBlank() && key.isNotBlank()
            TalosFormMode.OMNI -> omniUrl.isNotBlank() && cluster.isNotBlank() && identity.isNotBlank()
        }

    /** The form as BuildTalosconfig reads it: only the mode's fields, trimmed. */
    fun toJson(): String = buildJsonObject {
        put("mode", mode.id)
        put("name", name.trim())
        when (mode) {
            TalosFormMode.DIRECT -> {
                put("endpoints", JsonArray(endpointList.map(::JsonPrimitive)))
                put("nodes", JsonArray(nodeList.map(::JsonPrimitive)))
                put("ca", ca.trim())
                put("crt", crt.trim())
                put("key", key.trim())
            }
            TalosFormMode.OMNI -> {
                put("omniUrl", omniUrl.trim())
                put("cluster", cluster.trim())
                put("identity", identity.trim())
            }
        }
    }.toString()

    // Holds the client key: never in a log line.
    override fun toString() = "TalosForm(${mode.id}, $name)"
}

private val ENTRY_SEPARATORS = Regex("""[\s,]+""")

private fun splitEntries(text: String): List<String> = text.split(ENTRY_SEPARATORS).map { it.trim() }.filter { it.isNotEmpty() }
