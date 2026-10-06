package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames

/**
 * The Kubernetes API address the user set for clusters, by context fingerprint, to use
 * instead of the one in the kubeconfig Talos issues (a port forward, a load balancer, a
 * public name). Already checked by Ichorgo.normalizeKubeServer. Only on this device.
 */
class KubeServers(prefs: SharedPreferences) {
    // Committed synchronously: the worker reads it back at process start.
    private val map = FingerprintPrefsMap.strings(prefs, commit = true)
    val servers: StateFlow<Map<String, String>> get() = map.values

    /** The address set for [stored]'s active cluster, "" for the kubeconfig's own. */
    fun serverFor(stored: StoredConfig): String = stored.activeSummary?.fingerprint?.let { servers.value[it] }.orEmpty()

    /** Forgets the addresses of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = map.store(keepClusterNames(map.values.value, summary.contexts.map { it.fingerprint }))

    /** Sets the cluster [fingerprint]'s address; a blank one goes back to the kubeconfig's. */
    fun set(fingerprint: String, server: String) = map.set(fingerprint, server.takeUnless { it.isBlank() })

    companion object {
        const val FILE = "ichor-kube-servers"
    }
}
