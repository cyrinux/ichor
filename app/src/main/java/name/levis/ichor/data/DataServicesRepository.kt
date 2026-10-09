package name.levis.ichor.data

import name.levis.ichor.model.CertDetails
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.GarageBlockReport
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageRepairResult
import name.levis.ichor.model.LonghornAction
import name.levis.ichorgo.Ichorgo

/** Longhorn, Garage, CloudNativePG and cert-manager: their health and the actions on them. */
class DataServicesRepository(go: GoCall) : GoRepository(go) {
    /**
     * Health of Longhorn, Garage and CloudNativePG (os:admin). [hints]: their catalog ids seen in
     * the inventory (see [name.levis.ichor.model.dataServiceHints]); "" checks everything.
     */
    suspend fun dataServices(hints: String): DataServices = go.remember(DATA_SERVICES) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(DataServices.serializer(), Ichorgo.kubeDataServices(cfg, ctx, server, hints)) }
    }

    /** What the blocks failing to resync in a Garage cluster are, run in its ready pod (os:admin). Never cached. */
    suspend fun garageBlockErrors(instance: GarageInstance): GarageBlockReport = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageBlockReport.serializer(), Ichorgo.kubeGarageBlockErrors(cfg, ctx, server, instance.namespace, instance.pod))
    }

    /** Launches the safe repairs for those blocks (os:admin); Garage runs them in the background. */
    suspend fun garageRepairBlocks(instance: GarageInstance): GarageRepairResult = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageRepairResult.serializer(), Ichorgo.kubeGarageRepairBlocks(cfg, ctx, server, instance.namespace, instance.pod))
    }

    /** Sets a node's resync tranquility (os:admin); [nodeId] is a Garage node ID, or "*" for every node. */
    suspend fun garageSetTranquility(instance: GarageInstance, nodeId: String, value: Long) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeGarageSetTranquility(cfg, ctx, server, instance.namespace, instance.pod, nodeId, value)
    }

    /**
     * What explains the state of the cert-manager certificate [namespace]/[name] (os:admin): its
     * requests, ACME orders and challenges, their events and controller log lines. Never cached.
     */
    suspend fun certificateDetails(namespace: String, name: String): CertDetails = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(CertDetails.serializer(), Ichorgo.kubeCertManagerDetails(cfg, ctx, server, namespace, name))
    }

    /**
     * Issues the cert-manager certificate [namespace]/[name] again now, like `cmctl renew`
     * (os:admin). Throws when refused, also while it is already being issued.
     */
    suspend fun renewCertificate(namespace: String, name: String) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeCertManagerRenew(cfg, ctx, server, namespace, name)
    }

    /**
     * Starts a backup of the CloudNativePG cluster [namespace]/[name] now, like `kubectl cnpg backup`
     * (os:admin): the new Backup's name. Throws when refused (hibernated, no backup method, one running).
     */
    suspend fun cnpgBackup(namespace: String, name: String): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubeCNPGBackup(cfg, ctx, server, namespace, name)
    }

    /**
     * Runs [action] on the Longhorn volume or node [namespace]/[name] (os:admin); [value] is the
     * replica count of [LonghornAction.REPLICAS]. Throws when refused.
     */
    suspend fun longhornAction(namespace: String, name: String, action: LonghornAction, value: Int = 0) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeLonghornAction(cfg, ctx, server, namespace, name, action.wire, value.toLong())
    }
}
