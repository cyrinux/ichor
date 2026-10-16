package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.PolicyReport
import name.levis.ichorgo.Ichorgo

/** Kyverno's policies and Gatekeeper's constraints with their findings, through the Kubernetes API (os:admin). */
class PolicyRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    /** Every policy with its readiness and findings, and the engine's health. */
    suspend fun policies(): PolicyReport = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        TalosJson.decodeFromString(PolicyReport.serializer(), Ichorgo.kubePolicies(target.yaml, target.context, target.server))
    }

    /**
     * Switches the Kyverno policy [name] ([namespace] empty: a ClusterPolicy) to [action], Audit or
     * Enforce, its rules' failureAction with it. Refused on the demo and without patch access.
     */
    suspend fun setAction(namespace: String, name: String, action: String) {
        withContext(Dispatchers.IO) {
            val target = kubeServers.targetFor(configs.forCall())
            Ichorgo.kubePolicySetAction(target.yaml, target.context, target.server, namespace, name, action)
        }
    }
}
