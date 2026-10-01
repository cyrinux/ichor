package name.levis.talosmobile.model

/** Longest name one can give a cluster: it titles the overview and labels a launcher shortcut. */
const val CLUSTER_NAME_MAX = 40

/** A name typed for a cluster, trimmed; null (back to its talosconfig context name) when blank. */
fun normalizeClusterName(input: String): String? =
    input.trim().take(CLUSTER_NAME_MAX).trim().takeIf { it.isNotEmpty() }

/** The names of [saved] (by fingerprint) whose cluster is still among [fingerprints]. */
fun keepClusterNames(saved: Map<String, String>, fingerprints: List<String>): Map<String, String> {
    val known = fingerprints.filter { it.isNotBlank() }.toSet()
    return saved.filterKeys { it in known }
}

/**
 * How clusters are called on screen: the name the user gave one (by fingerprint, kept on
 * this device only), else its talosconfig context name. Screenshot mode masks context
 * names, so given names are not shown then: they may be just as revealing.
 */
data class ClusterLabels(val names: Map<String, String> = emptyMap(), val masked: Boolean = false) {
    fun of(context: ContextSummary): String = given(context) ?: context.name

    /** The name the user gave [context], if any and if it may be shown. */
    fun given(context: ContextSummary): String? =
        if (masked || context.fingerprint.isBlank()) null else names[context.fingerprint]
}
