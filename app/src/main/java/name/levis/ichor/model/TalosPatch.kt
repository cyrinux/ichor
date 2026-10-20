package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/talos_patch.go.

/** One document of a Talos patch file: [kind] is v1alpha1 for machine/cluster sections. */
@Serializable
data class TalosPatchDoc(val kind: String = "", val name: String = "", val hostname: String = "")

/**
 * What a repository file is to Talos: [talos] when it is a patch (or a whole config); it can be
 * tried on a node when [tryable], else [note] says why (a secret, a whole config, a talenv
 * placeholder).
 */
@Serializable
data class TalosPatchInfo(
    val talos: Boolean = false,
    /** patch, documents, jsonpatch or config. */
    val kind: String = "",
    val documents: List<TalosPatchDoc> = emptyList(),
    val touchesSecrets: Boolean = false,
    val secret: String = "",
    val tryable: Boolean = false,
    val note: String = "",
) {
    /** The hostnames the file sets: the node it is meant for. */
    val hostnames: List<String> get() = documents.map { it.hostname }.filter { it.isNotEmpty() }.distinct()
}

/** A node's config with the patch applied, redacted: the editor's draft. */
@Serializable
data class TalosPatchDraft(val draft: String = "", val same: Boolean = false, val needsReboot: Boolean = false)

/**
 * A patch file to try on a node, carried to the machine config screen: its [content] and where
 * it comes from ([from], "path @ commit"), shown above the review.
 */
data class PatchPrefill(val content: String, val from: String)

/** A file the size of a patch: larger ones are not carried through navigation. */
const val MAX_PATCH_BYTES = 32 * 1024

/**
 * The node a patch is meant for among [nodes], by the hostname it sets (an FQDN matching its
 * short name too), null when it names none of them.
 */
fun patchNode(info: TalosPatchInfo, nodes: List<NodeOverview>): NodeOverview? {
    val wanted = info.hostnames.ifEmpty { return null }
    fun short(h: String) = h.substringBefore('.').lowercase()
    return nodes.firstOrNull { n -> wanted.any { it.equals(n.hostname, ignoreCase = true) } }
        ?: nodes.firstOrNull { n -> wanted.any { short(it) == short(n.hostname) } }
}
