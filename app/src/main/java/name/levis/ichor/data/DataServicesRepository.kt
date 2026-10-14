package name.levis.ichor.data

import kotlinx.serialization.encodeToString
import name.levis.ichor.model.LonghornAttachmentPreview
import name.levis.ichor.model.LonghornSalvagePreview
import name.levis.ichor.model.VolumeSnapshots
import name.levis.ichor.model.CnpgPromotePreview
import name.levis.ichor.model.CertDetails
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.GarageBlockReport
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageRepairResult
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.VeleroBackups
import name.levis.ichor.model.VeleroRestoreOptions
import name.levis.ichor.model.VeleroRestorePreview
import name.levis.ichorgo.Ichorgo

/** Longhorn, Garage, CloudNativePG and cert-manager: their health and the actions on them. */
class DataServicesRepository(go: GoCall) : GoRepository(go) {
 suspend fun longhornAttachmentPreview(namespace: String, name: String): LonghornAttachmentPreview = go.kube { cfg, ctx, server -> TalosJson.decodeFromString(LonghornAttachmentPreview.serializer(), Ichorgo.kubeLonghornAttachmentPreview(cfg, ctx, server, namespace, name)) }
 suspend fun longhornAttachment(namespace: String, name: String, node: String, attach: Boolean) = go.kube { cfg, ctx, server -> Ichorgo.kubeLonghornAttachment(cfg, ctx, server, namespace, name, node, attach) }

 suspend fun longhornSalvagePreview(namespace: String, name: String): LonghornSalvagePreview = go.kube { cfg, ctx, server -> TalosJson.decodeFromString(LonghornSalvagePreview.serializer(), Ichorgo.kubeLonghornSalvagePreview(cfg, ctx, server, namespace, name)) }
 suspend fun longhornSalvage(namespace: String, name: String, replicas: Set<String>) = go.kube { cfg, ctx, server -> Ichorgo.kubeLonghornSalvage(cfg, ctx, server, namespace, name, TalosJson.encodeToString(replicas.toList())) }

    suspend fun volumeSnapshots(namespace: String = ""): VolumeSnapshots = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(VolumeSnapshots.serializer(), Ichorgo.kubeVolumeSnapshots(cfg, ctx, server, namespace))
    }

    suspend fun createVolumeSnapshot(namespace: String, pvc: String): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubeCreateVolumeSnapshot(cfg, ctx, server, namespace, pvc)
    }
 suspend fun cnpgRestartPrimary(namespace: String, name: String) = go.kube { cfg, ctx, server -> Ichorgo.kubeCNPGRestartPrimary(cfg, ctx, server, namespace, name) }
 suspend fun cnpgHibernate(namespace: String, name: String, enabled: Boolean) = go.kube { cfg, ctx, server -> Ichorgo.kubeCNPGHibernate(cfg, ctx, server, namespace, name, enabled) }

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

    suspend fun cnpgPromotePreview(namespace: String, name: String, instance: String): CnpgPromotePreview = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(CnpgPromotePreview.serializer(), Ichorgo.kubeCNPGPromotePreview(cfg, ctx, server, namespace, name, instance))
    }

    suspend fun cnpgPromote(namespace: String, name: String, instance: String) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeCNPGPromote(cfg, ctx, server, namespace, name, instance)
    }

    /** The backups of the Velero schedule [namespace]/[schedule], or the ones taken by hand when it is "" (os:admin). Never cached. */
    suspend fun veleroBackups(namespace: String, schedule: String): VeleroBackups = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(VeleroBackups.serializer(), Ichorgo.kubeVeleroBackups(cfg, ctx, server, namespace, schedule))
    }

    /**
     * Backs the Velero schedule [namespace]/[schedule] up now, like `velero backup create --from-schedule`
     * (os:admin): the new Backup's name. Throws when refused (paused, invalid, a backup running).
     */
    suspend fun veleroBackupNow(namespace: String, schedule: String): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubeVeleroBackupNow(cfg, ctx, server, namespace, schedule)
    }

    /**
     * Asks Velero to delete the backup [namespace]/[backup] and its data, like `velero backup delete`
     * (os:admin): the deletion request's name. Throws when refused (still running, already deleting).
     */
    suspend fun veleroDeleteBackup(namespace: String, backup: String): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubeVeleroDeleteBackup(cfg, ctx, server, namespace, backup)
    }

    /**
     * What restoring the Velero backup [namespace]/[backup] holds (os:admin): its namespaces,
     * items and volumes, whether it can be restored, and the Restores of it already there.
     * Never cached: the same read follows a restore's progress.
     */
    suspend fun veleroRestorePreview(namespace: String, backup: String): VeleroRestorePreview = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(VeleroRestorePreview.serializer(), Ichorgo.kubeVeleroRestorePreview(cfg, ctx, server, namespace, backup))
    }

    /**
     * Restores the Velero backup [namespace]/[backup] with [options], like
     * `velero restore create --from-backup` (os:admin): the Restore's name. Throws when refused
     * (the backup cannot be a source, its storage location is down, a restore is running).
     */
    suspend fun veleroRestore(namespace: String, backup: String, options: VeleroRestoreOptions): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubeVeleroRestore(cfg, ctx, server, namespace, backup, TalosJson.encodeToString(VeleroRestoreOptions.serializer(), options))
    }

    /**
     * Runs [action] on the Longhorn volume or node [namespace]/[name] (os:admin); [value] is the
     * replica count of [LonghornAction.REPLICAS]. Throws when refused.
     */
    suspend fun longhornAction(namespace: String, name: String, action: LonghornAction, value: Int = 0) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeLonghornAction(cfg, ctx, server, namespace, name, action.wire, value.toLong())
    }
}
