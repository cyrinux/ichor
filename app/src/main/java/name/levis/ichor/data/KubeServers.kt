package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames

/**
 * The Kubernetes API address the user set for clusters, by context fingerprint, to use
 * instead of the one in the kubeconfig Talos issues (a port forward, a load balancer, a
 * public name). Already checked by Ichorgo.normalizeKubeServer. Only on this device.
 */
class KubeServers(private val prefs: SharedPreferences) {
    private val _servers = MutableStateFlow(
        prefs.all.mapNotNull { (fingerprint, server) -> (server as? String)?.let { fingerprint to it } }.toMap(),
    )
    val servers: StateFlow<Map<String, String>> = _servers.asStateFlow()

    /** The address set for [stored]'s active cluster, "" for the kubeconfig's own. */
    fun serverFor(stored: StoredConfig): String = stored.activeSummary?.fingerprint?.let { servers.value[it] }.orEmpty()

    /** Forgets the addresses of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = store(keepClusterNames(_servers.value, summary.contexts.map { it.fingerprint }))

    /** Sets the cluster [fingerprint]'s address; a blank one goes back to the kubeconfig's. */
    fun set(fingerprint: String, server: String) {
        if (fingerprint.isBlank()) return
        store(if (server.isBlank()) _servers.value - fingerprint else _servers.value + (fingerprint to server))
    }

    /** Committed synchronously: the worker reads it back at process start. */
    private fun store(servers: Map<String, String>) {
        if (servers == _servers.value) return
        prefs.edit().clear().also { editor -> servers.forEach { (fingerprint, server) -> editor.putString(fingerprint, server) } }.commit()
        _servers.value = servers
    }

    companion object {
        const val FILE = "ichor-kube-servers"
    }
}
