package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/configsnippets.go.

/** A parameter of a config snippet; [hint] is an example value (verbatim). */
@Serializable
data class SnippetParam(
    val name: String,
    /** text, list (one per line), int or bool. */
    val type: String = "text",
    val hint: String = "",
)

/** A ready-made machine config change; the apps translate its [id] and [group]. */
@Serializable
data class ConfigSnippet(
    val id: String,
    val group: String,
    val params: List<SnippetParam> = emptyList(),
    /** Takes effect at the next boot only: it cannot be tried. */
    val staged: Boolean = false,
)

@Serializable
data class ConfigSnippetList(val snippets: List<ConfigSnippet> = emptyList())

/** The edits a snippet or a drift fix makes to the draft; [reason] says why when not [fixable]. */
@Serializable
data class SnippetEdits(
    val fixable: Boolean = false,
    val reason: String = "",
    val edits: List<ConfigEdit> = emptyList(),
    val staged: Boolean = false,
)

/** A drift difference to undo with a config change: the node gets the reference's value [before]. */
data class DriftFix(val key: String, val before: String, val after: String)

/** A snippet to open the picker on, its [params] prefilled (from another screen: a pull hint). */
data class SnippetPrefill(val id: String, val params: Map<String, String> = emptyMap()) {
    /** [params] as one string for a route argument: one `name=value` per line. */
    fun encodedParams(): String = params.entries.joinToString("\n") { (k, v) -> "$k=$v" }

    companion object {
        /** The prefill of route arguments [id] and [params] ([encodedParams]); null without an id. */
        fun decode(id: String, params: String): SnippetPrefill? = id.takeIf { it.isNotEmpty() }?.let {
            SnippetPrefill(
                it,
                params.lines().mapNotNull { line -> line.split('=', limit = 2).takeIf { p -> p.size == 2 && p[0].isNotEmpty() }?.let { p -> p[0] to p[1] } }.toMap(),
            )
        }
    }
}

/** The drift keys a machine config change can undo (DriftFixEdits); the others come from elsewhere. */
private val fixableDriftKeys = setOf("dns", "ntp", "mtu")

/** The fix of this difference, or null when no config change can undo it. */
fun DriftChange.fix(): DriftFix? = DriftFix(key, before, after).takeIf { key in fixableDriftKeys && before.isNotBlank() }

/** The snippets of each group, groups in the order the core lists them. */
fun List<ConfigSnippet>.byGroup(): List<Pair<String, List<ConfigSnippet>>> = groupBy { it.group }.toList()
