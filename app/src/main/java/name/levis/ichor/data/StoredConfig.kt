package name.levis.ichor.data

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.isKube

/**
 * The stored configs plus the context the user is looking at. Talos clusters live in one
 * talosconfig ([talosYaml]), clusters added from a kubeconfig in one kubeconfig ([kubeYaml]);
 * either is "" when it holds no cluster, never both. [summary] lists the Talos contexts, then
 * the kubeconfig ones: one list for the switcher, names unique across both.
 */
data class StoredConfig(
    val talosYaml: String,
    val kubeYaml: String,
    val summary: ConfigSummary,
    val activeContext: String,
) {
    /**
     * The config holding the active context: what a call for the cluster on screen passes to
     * the Go core with [activeContext]. The core tells a kubeconfig from a talosconfig, so the
     * Kubernetes calls work for both and the Talos ones refuse a kubeconfig cluster.
     */
    val yaml: String get() = yamlFor(activeContext)

    /** The config holding [context]: the kubeconfig for a cluster added from one, else the talosconfig. */
    fun yamlFor(context: String): String = if (summary.find(context)?.isKube == true) kubeYaml else talosYaml

    // Holds client keys: never in a log line.
    override fun toString() = "StoredConfig(${summary.contexts.size} contexts, active=$activeContext)"
}

val StoredConfig.activeSummary: ContextSummary?
    get() = summary.find(activeContext)

/** The active cluster's fingerprint, to key its state: null for the demo or when unknown. */
val StoredConfig.realFingerprint: String?
    get() = activeSummary?.takeUnless { it.isDemo }?.fingerprint?.takeIf { it.isNotBlank() }

/** The cluster on screen was added from a kubeconfig: no Talos API. */
val StoredConfig.activeIsKube: Boolean
    get() = activeSummary?.isKube == true

private fun ConfigSummary.find(name: String): ContextSummary? = contexts.firstOrNull { it.name == name }

/**
 * One list of the stored clusters: the talosconfig's contexts, then the kubeconfig's. The
 * current one is the talosconfig's, else the kubeconfig's (what shows when nothing was picked).
 */
internal fun mergeSummaries(talos: ConfigSummary?, kube: ConfigSummary?): ConfigSummary = ConfigSummary(
    current = talos?.current?.takeIf { it.isNotEmpty() } ?: kube?.current.orEmpty(),
    contexts = talos?.contexts.orEmpty() + kube?.contexts.orEmpty(),
)

/** How many contexts of [summary] the talosconfig holds (they come first). */
internal fun talosContextCount(summary: ConfigSummary): Int = summary.contexts.count { !it.isKube }
