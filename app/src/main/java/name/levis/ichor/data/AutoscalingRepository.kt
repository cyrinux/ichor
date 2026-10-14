package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.AutoscalingReport
import name.levis.ichorgo.Ichorgo

/** What scales the cluster: HPAs, KEDA, VPA recommendations and Karpenter, through the Kubernetes API (os:admin). */
class AutoscalingRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    /** Every autoscaler's rows with their states, and the per-workload index. */
    suspend fun autoscaling(): AutoscalingReport = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        TalosJson.decodeFromString(AutoscalingReport.serializer(), Ichorgo.kubeAutoscaling(target.yaml, target.context, target.server))
    }
}
