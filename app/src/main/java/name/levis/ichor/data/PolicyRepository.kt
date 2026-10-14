package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.PolicyReport
import name.levis.ichorgo.Ichorgo

/** Kyverno's admission policies and their PolicyReport findings, through the Kubernetes API (os:admin). */
class PolicyRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    /** Every policy with its readiness and findings, and the engine's health. */
    suspend fun policies(): PolicyReport = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        TalosJson.decodeFromString(PolicyReport.serializer(), Ichorgo.kubePolicies(target.yaml, target.context, target.server))
    }
}
