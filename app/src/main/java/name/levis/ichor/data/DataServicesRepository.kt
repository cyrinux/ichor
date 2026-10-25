package name.levis.ichor.data

import kotlinx.serialization.encodeToString
import name.levis.ichor.model.LonghornAttachmentPreview
import name.levis.ichor.model.LonghornEngineUpgradePreview
import name.levis.ichor.model.LonghornSalvagePreview
import name.levis.ichor.model.VolumeSnapshots
import name.levis.ichor.model.CnpgPromotePreview
import name.levis.ichor.model.CertDetails
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.GarageBlockReport
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageLayout
import name.levis.ichor.model.KubeGreenNamespace
import name.levis.ichor.model.GarageRepairResult
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.LonghornSnapshotAction
import name.levis.ichor.model.VeleroBackups
import name.levis.ichor.model.VeleroRestoreOptions
import name.levis.ichor.model.VeleroRestorePreview
import name.levis.ichor.model.isDemo
import name.levis.ichorgo.Ichorgo

/** A minute: how long a namespace's kube-green state is reused while sheets open and close. */
private const val KUBE_GREEN_FRESH_MILLIS = 60_000L

/** Longhorn, Garage, CloudNativePG and cert-manager: their health and the actions on them. */
class DataServicesRepository(go: GoCall) : GoRepository(go) {
 suspend fun longhornAttachmentPreview(namespace: String, name: String): LonghornAttachmentPreview = go.kube { cfg, ctx, server -> TalosJson.decodeFromString(LonghornAttachmentPreview.serializer(), Ichorgo.kubeLonghornAttachmentPreview(cfg, ctx, server, namespace, name)) }
 suspend fun longhornAttachment(namespace: String, name: String, node: String, attach: Boolean) = go.kube { cfg, ctx, server -> Ichorgo.kubeLonghornAttachment(cfg, ctx, server, namespace, name, node, attach) }

 suspend fun longhornEngineUpgradePreview(namespace: String, name: String): LonghornEngineUpgradePreview = go.kube { cfg, ctx, server -> TalosJson.decodeFromString(LonghornEngineUpgradePreview.serializer(), Ichorgo.kubeLonghornEngineUpgradePreview(cfg, ctx, server, namespace, name)) }
 suspend fun longhornEngineUpgrade(namespace: String, name: String, image: String) = go.kube { cfg, ctx, server -> Ichorgo.kubeLonghornEngineUpgrade(cfg, ctx, server, namespace, name, image) }
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

    /**
     * What kube-green has done to [namespace] (os:admin): whether it is asleep, what its
     * SleepInfos suspend and when. Read when a workload or CronJob sheet opens, kept for a
     * minute per namespace so opening several rows costs one call; a refusal (403) throws and
     * the caller shows nothing.
     */
    suspend fun kubeGreenNamespace(namespace: String): KubeGreenNamespace = go.remember(kubeGreenNamespaceKey(namespace)) { last ->
        val fresh = last()
        if (fresh != null && System.currentTimeMillis() - fresh.at < KUBE_GREEN_FRESH_MILLIS) {
            fresh.value
        } else {
            go.kube { cfg, ctx, server ->
                TalosJson.decodeFromString(KubeGreenNamespace.serializer(), Ichorgo.kubeGreenNamespace(cfg, ctx, server, namespace))
            }
        }
    }

    /** Drops [namespace]'s kube-green state kept for a minute: the next read asks the cluster. */
    fun forgetKubeGreenNamespace(namespace: String) = go.results.forget(kubeGreenNamespaceKey(namespace))

    /** What the blocks failing to resync in a Garage cluster are, run in its ready pod (os:admin). Never cached. */
    suspend fun garageBlockErrors(instance: GarageInstance): GarageBlockReport = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageBlockReport.serializer(), Ichorgo.kubeGarageBlockErrors(cfg, ctx, server, instance.namespace, instance.pod))
    }

    /** Launches the safe repairs for those blocks (os:admin); Garage runs them in the background. */
    suspend fun garageRepairBlocks(instance: GarageInstance): GarageRepairResult = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageRepairResult.serializer(), Ichorgo.kubeGarageRepairBlocks(cfg, ctx, server, instance.namespace, instance.pod))
    }

    /** A Garage cluster's layout: roles, staged changes with their preview, version history (os:admin). Never cached. */
    suspend fun garageLayout(instance: GarageInstance): GarageLayout = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageLayout.serializer(), Ichorgo.kubeGarageLayout(cfg, ctx, server, instance.namespace, instance.pod))
    }

    /**
     * Stages a role for the Garage node [nodeId] (a full id): [zone], [capacity] in bytes unless
     * [gateway], and [tags] (os:admin). Nothing moves until [garageApplyLayout]. Answers the layout
     * with the staged change and its preview.
     */
    suspend fun garageStageRole(instance: GarageInstance, nodeId: String, zone: String, capacity: Long, gateway: Boolean, tags: List<String>): GarageLayout =
        go.kube { cfg, ctx, server ->
            TalosJson.decodeFromString(
                GarageLayout.serializer(),
                Ichorgo.kubeGarageStageRole(cfg, ctx, server, instance.namespace, instance.pod, nodeId, zone, capacity, gateway, TalosJson.encodeToString(tags)),
            )
        }

    /** Stages the removal of the Garage node [nodeId] from the layout (os:admin). */
    suspend fun garageStageRemoval(instance: GarageInstance, nodeId: String): GarageLayout = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageLayout.serializer(), Ichorgo.kubeGarageStageRemoval(cfg, ctx, server, instance.namespace, instance.pod, nodeId))
    }

    /** Stages the layout's zone redundancy: "maximum" or a number of zones (os:admin). */
    suspend fun garageSetZoneRedundancy(instance: GarageInstance, value: String): GarageLayout = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageLayout.serializer(), Ichorgo.kubeGarageSetZoneRedundancy(cfg, ctx, server, instance.namespace, instance.pod, value))
    }

    /** Applies the staged layout changes as [version], the one the preview announced (os:admin); Garage refuses any other. */
    suspend fun garageApplyLayout(instance: GarageInstance, version: Long): GarageLayout = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageLayout.serializer(), Ichorgo.kubeGarageApplyLayout(cfg, ctx, server, instance.namespace, instance.pod, version))
    }

    /**
     * Lets a draining layout version stop waiting on the nodes that are down (os:admin): their
     * acknowledgement trackers move to [version], the current one. [allowMissingData] moves every
     * node's sync tracker too, giving up the metadata only the dead nodes held. Throws when nothing moved.
     */
    suspend fun garageSkipDeadNodes(instance: GarageInstance, version: Long, allowMissingData: Boolean): GarageLayout = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageLayout.serializer(), Ichorgo.kubeGarageSkipDeadNodes(cfg, ctx, server, instance.namespace, instance.pod, version, allowMissingData))
    }

    /** Drops every staged layout change (os:admin). */
    suspend fun garageRevertLayout(instance: GarageInstance): GarageLayout = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageLayout.serializer(), Ichorgo.kubeGarageRevertLayout(cfg, ctx, server, instance.namespace, instance.pod))
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
     * Moves the kube-green SleepInfo [namespace]/[name]'s schedule: [weekdays] (cron day-of-week),
     * [sleepAt] and [wakeUpAt] ("HH:mm"; "" for no wake-up) in [timeZone] (IANA). Throws when refused.
     */
    suspend fun setKubeGreenSchedule(namespace: String, name: String, weekdays: String, sleepAt: String, wakeUpAt: String, timeZone: String) =
        go.kube { cfg, ctx, server ->
            Ichorgo.kubeGreenSetSchedule(cfg, ctx, server, namespace, name, weekdays, sleepAt, wakeUpAt, timeZone)
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

    /**
     * Runs [action] on [snapshot] ("" to take one) of the Longhorn volume [namespace]/[volume]
     * (os:admin). Throws when refused, a detached volume included.
     */
    suspend fun longhornSnapshotAction(namespace: String, volume: String, snapshot: String, action: LonghornSnapshotAction) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeLonghornSnapshotAction(cfg, ctx, server, namespace, volume, snapshot, action.wire)
    }
}
