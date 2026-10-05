package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_namespaces.go.

@Serializable
data class KubeNamespaces(
    val namespaces: List<String> = emptyList(),
    /** The credentials may not list namespaces: the user types one instead. */
    val forbidden: Boolean = false,
    /** The kubeconfig context's namespace, "" when it sets none. */
    val contextNamespace: String = "",
)

/**
 * The namespace the Kubernetes lists are loaded for (L5): [namespace] null for every one.
 * [chosen]: the user picked it (remembered per cluster), rather than the default.
 */
data class KubeScope(val namespace: String? = null, val chosen: Boolean = false) {
    /** How it is remembered: null when not [chosen], "" for every namespace. */
    val stored: String? get() = if (chosen) namespace.orEmpty() else null

    companion object {
        /** The scope remembered as [value] (see [stored]); the default when null. */
        fun fromStored(value: String?): KubeScope = when (value) {
            null -> KubeScope()
            "" -> KubeScope(chosen = true)
            else -> KubeScope(value, chosen = true)
        }
    }
}

/**
 * The scope to load (L5, L6): the [remembered] one, else every namespace; when namespaces
 * cannot be listed, the kubeconfig context's namespace (listing every namespace would be
 * refused too).
 */
fun defaultScope(remembered: KubeScope?, namespaces: KubeNamespaces?): KubeScope = when {
    remembered != null -> remembered
    namespaces?.forbidden == true && namespaces.contextNamespace.isNotBlank() -> KubeScope(namespaces.contextNamespace)
    else -> KubeScope()
}

/**
 * How many rows the eager load of [scope] reads (L5, L8). Every namespace by default stops
 * after its first page: when that page shows a large cluster, the user picks a namespace (or
 * chooses every namespace, then loaded up to [eagerCap]). A small cluster fits in that page.
 */
fun eagerLimit(scope: KubeScope, metered: Boolean): Int =
    if (scope.namespace == null && !scope.chosen) KUBE_PAGE_SIZE else eagerCap(metered)

/** The namespaces to offer: the listed ones, else those of the loaded rows; [scope]'s always. */
fun scopeChoices(listed: KubeNamespaces?, loaded: List<String>, scope: KubeScope): List<String> {
    val base = listed?.namespaces?.takeIf { it.isNotEmpty() } ?: loaded
    return (base + listOfNotNull(scope.namespace)).distinct().sorted()
}

/** Above this many namespaces the picker is a searchable sheet instead of chips. */
const val SCOPE_CHIPS_MAX = 15

/** [namespaces] containing [query] (case-insensitive). */
fun List<String>.matchingNamespaces(query: String): List<String> {
    val q = query.trim()
    return if (q.isEmpty()) this else filter { it.contains(q, ignoreCase = true) }
}
