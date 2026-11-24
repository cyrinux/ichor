package name.levis.ichor.data

import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.withContext
import name.levis.ichor.model.AutoscalingReport
import name.levis.ichor.model.NodeGroupUpdate
import name.levis.ichor.model.NodeGroupsReport
import name.levis.ichorgo.Ichorgo

/** What scales the cluster: HPAs, KEDA, VPA recommendations and Karpenter, through the Kubernetes API (os:admin). */
class AutoscalingRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    /** Every autoscaler's rows with their states, the per-workload index, and the cloud's node groups. */
    suspend fun autoscaling(): AutoscalingReport = coroutineScope {
        val groups = async { nodeGroups() }
        val report = withContext(Dispatchers.IO) {
            val target = kubeServers.targetFor(configs.forCall())
            TalosJson.decodeFromString(AutoscalingReport.serializer(), Ichorgo.kubeAutoscaling(target.yaml, target.context, target.server))
        }
        report.copy(nodeGroups = groups.await())
    }

    /**
     * The cluster's EKS managed node groups, read with its own AWS sign-in. A failed read is the
     * group's error line: the rest of the Autoscaling screen does not depend on AWS.
     */
    suspend fun nodeGroups(): NodeGroupsReport = withContext(Dispatchers.IO) {
        try {
            val target = kubeServers.targetFor(configs.forCall())
            TalosJson.decodeFromString(NodeGroupsReport.serializer(), Ichorgo.kubeNodeGroups(target.yaml, target.context, target.server))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            NodeGroupsReport(error = e.message.orEmpty())
        }
    }

    /** Pauses the ScaledObject at [replicas], holds it at its count (-1) or resumes it (-2). */
    suspend fun pauseScaledObject(namespace: String, name: String, replicas: Int) = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        Ichorgo.kubePauseScaledObject(target.yaml, target.context, target.server, namespace, name, replicas.toLong())
    }

    /** Sets the HPA's minReplicas and maxReplicas. */
    suspend fun setHpaBounds(namespace: String, name: String, min: Int, max: Int) = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        Ichorgo.kubeSetHPABounds(target.yaml, target.context, target.server, namespace, name, min.toLong(), max.toLong())
    }

    /** Changes the EKS managed node group's size; the update AWS starts for it. */
    suspend fun scaleNodeGroup(group: String, desired: Int, min: Int, max: Int): NodeGroupUpdate = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        TalosJson.decodeFromString(
            NodeGroupUpdate.serializer(),
            Ichorgo.kubeNodeGroupScale(target.yaml, target.context, target.server, group, desired.toLong(), min.toLong(), max.toLong()),
        )
    }

    /**
     * Upgrades the EKS managed node group to the control plane's [version], or to the latest
     * node image of its own when [version] is empty; [force] ignores pod disruption budgets.
     * The update AWS starts for it.
     */
    suspend fun nodeGroupUpgrade(group: String, version: String, force: Boolean): NodeGroupUpdate = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        TalosJson.decodeFromString(
            NodeGroupUpdate.serializer(),
            Ichorgo.kubeNodeGroupUpgrade(target.yaml, target.context, target.server, group, version, force),
        )
    }

    /** The update [id] of [group], as AWS reports it now. */
    suspend fun nodeGroupUpdate(group: String, id: String): NodeGroupUpdate = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        TalosJson.decodeFromString(NodeGroupUpdate.serializer(), Ichorgo.kubeNodeGroupUpdate(target.yaml, target.context, target.server, group, id))
    }
}
