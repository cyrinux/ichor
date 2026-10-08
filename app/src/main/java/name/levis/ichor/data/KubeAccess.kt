package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.isKube

/**
 * The Kubernetes access of Talos clusters (K5): by Talos context fingerprint, the fingerprint
 * of the stored kubeconfig cluster whose credentials the Kubernetes calls use instead of the
 * admin kubeconfig Talos issues (an os:reader user, a team's OIDC identity). Talos calls are
 * unchanged. Only on this device, and in backups.
 */
class KubeAccess(prefs: SharedPreferences) {
    // Committed synchronously: the worker reads it back at process start.
    private val map = FingerprintPrefsMap.strings(prefs, commit = true)
    val links: StateFlow<Map<String, String>> get() = map.values

    /** Forgets the links of Talos clusters no longer in [summary], and those to a kubeconfig cluster gone. */
    fun sync(summary: ConfigSummary) = map.store(validKubeAccess(map.values.value, summary))

    /** Links the Talos cluster [fingerprint] to the kubeconfig cluster [kubeFingerprint]; null: the Talos admin kubeconfig. */
    fun set(fingerprint: String, kubeFingerprint: String?) = map.set(fingerprint, kubeFingerprint?.takeUnless { it.isBlank() })

    companion object {
        const val FILE = "ichor-kube-access"
    }
}

/**
 * The links of [links] from a Talos cluster of [summary] to a kubeconfig cluster of [summary].
 * An Omni cluster's Kubernetes goes through Omni, with its sign-in: no link applies to it.
 */
internal fun validKubeAccess(links: Map<String, String>, summary: ConfigSummary): Map<String, String> {
    val talos = summary.contexts.filter { !it.isKube && !it.demo && !it.omni }.map { it.fingerprint }.toSet()
    val kube = summary.contexts.filter { it.isKube }.map { it.fingerprint }.toSet()
    return links.filter { (from, to) -> from in talos && to in kube }
}

/** [summary] with each Talos context's [name.levis.ichor.model.ContextSummary.kubeAccess] set from [links]. */
internal fun withKubeAccess(summary: ConfigSummary, links: Map<String, String>): ConfigSummary {
    val valid = validKubeAccess(links, summary)
    return summary.copy(
        contexts = summary.contexts.map { context ->
            val link = if (context.isKube) "" else valid[context.fingerprint].orEmpty()
            if (link == context.kubeAccess) context else context.copy(kubeAccess = link)
        },
    )
}

/** Where a Kubernetes call of the cluster on screen goes: a config, its context, the API address set by the user ("" for the config's). */
data class KubeTarget(val yaml: String, val context: String, val server: String) {
    // Holds credentials: never in a log line.
    override fun toString() = "KubeTarget($context)"
}

/**
 * The Kubernetes target of [stored]'s active cluster: its own config, or for a Talos cluster
 * linked to a kubeconfig cluster (K5) the stored kubeconfig and that cluster's context.
 * [servers] are the API addresses set by fingerprint.
 */
fun kubeTarget(stored: StoredConfig, servers: Map<String, String>): KubeTarget {
    val active = stored.activeSummary
    val linked = active?.kubeAccess?.takeIf { it.isNotEmpty() && !active.isKube }
        ?.let { fingerprint -> stored.summary.contexts.firstOrNull { it.isKube && it.fingerprint == fingerprint } }
    return if (linked != null) {
        KubeTarget(stored.kubeYaml, linked.name, servers[linked.fingerprint].orEmpty())
    } else {
        KubeTarget(stored.yaml, stored.activeContext, active?.fingerprint?.let { servers[it] }.orEmpty())
    }
}

/**
 * The kubeconfig cluster to sign in to when a Kubernetes call of [stored]'s active cluster asks
 * for it: the active one itself, or the one a Talos cluster goes through (K5); null otherwise.
 */
fun signInContextFor(stored: StoredConfig): String? {
    val active = stored.activeSummary ?: return null
    val target = if (active.isKube || active.omni) active else stored.summary.contexts.firstOrNull { it.isKube && active.kubeAccess.isNotEmpty() && it.fingerprint == active.kubeAccess }
    return target?.takeIf { it.signIn.isNotEmpty() }?.name
}
