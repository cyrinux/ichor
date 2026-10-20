import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    func longhornAttachmentPreview(namespace: String, name: String) async throws -> LonghornAttachmentPreview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] in
            IchorgoKubeLonghornAttachmentPreview(config, context, server, namespace, name, $0)
        }
    }
    func longhornAttachment(namespace: String, name: String, node: String, attach: Bool) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeLonghornAttachment(config, context, server, namespace, name, node, attach, error)
        }
    }

    func longhornEngineUpgradePreview(namespace: String, name: String) async throws -> LonghornEngineUpgradePreview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] in
            IchorgoKubeLonghornEngineUpgradePreview(config, context, server, namespace, name, $0)
        }
    }
    func longhornEngineUpgrade(namespace: String, name: String, image: String) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeLonghornEngineUpgrade(config, context, server, namespace, name, image, error)
        }
    }

    func longhornSalvagePreview(namespace: String, name: String) async throws -> LonghornSalvagePreview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] in
            IchorgoKubeLonghornSalvagePreview(config, context, server, namespace, name, $0)
        }
    }
    func longhornSalvage(namespace: String, name: String, replicas: Set<String>) async throws {
        let names = String(decoding: try JSONEncoder().encode(replicas.sorted()), as: UTF8.self)
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeLonghornSalvage(config, context, server, namespace, name, names, error)
        }
    }

 func cnpgRestartPrimary(namespace: String, name: String) async throws {
 try await Self.run { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] error -> Void in
 _ = IchorgoKubeCNPGRestartPrimary(config, context, server, namespace, name, error)
 }
 }
 func cnpgHibernate(namespace: String, name: String, enabled: Bool) async throws {
 try await Self.run { [config = self.kubeConfig, context = self.kubeContext, server = self.kubeAPIServer] error -> Void in
 _ = IchorgoKubeCNPGHibernate(config, context, server, namespace, name, enabled, error)
 }
 }

    func cnpgPromotePreview(namespace: String, name: String, instance: String) async throws -> CnpgPromotePreview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeCNPGPromotePreview(config, context, kubeServer, namespace, name, instance, $0)
        }
    }
    func cnpgPromote(namespace: String, name: String, instance: String) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeCNPGPromote(config, context, kubeServer, namespace, name, instance, error)
        }
    }
    func volumeSnapshots(namespace: String = "") async throws -> VolumeSnapshots {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeVolumeSnapshots(config, context, kubeServer, namespace, $0)
        }
    }
    func createVolumeSnapshot(namespace: String, pvc: String) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeCreateVolumeSnapshot(config, context, kubeServer, namespace, pvc, error)
        }
    }
    /// What kube-green has done to one namespace, read when a workload or CronJob row is opened so
    /// it can say the namespace is asleep before a scale, a restart or a resume (os:admin). A
    /// cluster without kube-green, or a refused list, answers installed:false rather than failing.
    func kubeGreenNamespace(_ namespace: String) async throws -> KubeGreenNamespace {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeGreenNamespace(config, context, kubeServer, namespace, $0)
        }
    }

    /// Health of Longhorn, Garage and CloudNativePG (os:admin). hints: their catalog ids seen in the
    /// inventory (see dataServiceHints); "" checks everything.
    func dataServices(hints: String) async throws -> DataServices {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeDataServices(config, context, kubeServer, hints, $0) }
    }

    /// What the blocks failing to resync in a Garage cluster are, from a sample (os:admin).
    func garageBlockErrors(_ instance: GarageInstance) async throws -> GarageBlockReport {
        let (namespace, pod) = (instance.namespace, instance.pod)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeGarageBlockErrors(config, context, kubeServer, namespace, pod, $0)
        }
    }

    /// Launches the safe repairs for the blocks failing to resync (os:admin); they run asynchronously.
    func garageRepairBlocks(_ instance: GarageInstance) async throws -> GarageRepairResult {
        let (namespace, pod) = (instance.namespace, instance.pod)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeGarageRepairBlocks(config, context, kubeServer, namespace, pod, $0)
        }
    }

    /// Sets the resync tranquility of a Garage node, full id or "*" (os:admin): 0 is full speed, 2 the default.
    func garageSetTranquility(_ instance: GarageInstance, node: String, value: Int) async throws {
        let (namespace, pod) = (instance.namespace, instance.pod)
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeGarageSetTranquility(config, context, kubeServer, namespace, pod, node, value, error)
        }
    }

    /// Runs action on the Longhorn volume or node namespace/name (os:admin); namespace is Longhorn's
    /// own. value: the replica count for .replicas, ignored otherwise.
    func longhornAction(namespace: String, name: String, action: LonghornAction, value: Int = 0) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeLonghornAction(config, context, kubeServer, namespace, name, action.rawValue, value, error)
        }
    }

    /// Runs action on snapshot ("" to take one) of the Longhorn volume namespace/volume (os:admin).
    /// Throws when refused, a detached volume included.
    func longhornSnapshotAction(namespace: String, volume: String, snapshot: String, action: LonghornSnapshotAction) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeLonghornSnapshotAction(config, context, kubeServer, namespace, volume, snapshot, action.rawValue, error)
        }
    }

    /// What explains the state of the cert-manager certificate namespace/name (os:admin): its
    /// requests, ACME orders and challenges, their events and controller log lines.
    func certificateDetails(namespace: String, name: String) async throws -> CertDetails {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeCertManagerDetails(config, context, kubeServer, namespace, name, $0)
        }
    }

    /// Moves the kube-green SleepInfo namespace/name's schedule: `weekdays` (cron day-of-week),
    /// `sleepAt` and `wakeUpAt` ("HH:mm"; "" for no wake-up) in `timeZone` (IANA).
    func setKubeGreenSchedule(namespace: String, name: String, weekdays: String, sleepAt: String, wakeUpAt: String, timeZone: String) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeGreenSetSchedule(config, context, kubeServer, namespace, name, weekdays, sleepAt, wakeUpAt, timeZone, error)
        }
    }

    /// Issues the cert-manager certificate namespace/name again now, like `cmctl renew` (os:admin).
    /// Refused while it is already being issued.
    func renewCertificate(namespace: String, name: String) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeCertManagerRenew(config, context, kubeServer, namespace, name, error)
        }
    }

    /// Starts a backup of the CloudNativePG cluster namespace/name now, like `kubectl cnpg backup`
    /// (os:admin): the new Backup's name. Refused when hibernated, without a backup method, or while one runs.
    func cnpgBackup(namespace: String, name: String) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeCNPGBackup(config, context, kubeServer, namespace, name, error)
        }
    }

    /// The backups of the Velero schedule namespace/schedule, or the ones taken by hand when it is "" (os:admin).
    func veleroBackups(namespace: String, schedule: String) async throws -> VeleroBackups {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeVeleroBackups(config, context, kubeServer, namespace, schedule, $0)
        }
    }

    /// Backs the Velero schedule namespace/schedule up now, like `velero backup create --from-schedule`
    /// (os:admin): the new Backup's name. Refused when paused, invalid, or while one of its backups runs.
    func veleroBackupNow(namespace: String, schedule: String) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeVeleroBackupNow(config, context, kubeServer, namespace, schedule, error)
        }
    }

    /// Asks Velero to delete the backup namespace/backup and its data, like `velero backup delete`
    /// (os:admin): the deletion request's name. Refused while it runs or is already being deleted.
    func veleroDeleteBackup(namespace: String, backup: String) async throws -> String {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeVeleroDeleteBackup(config, context, kubeServer, namespace, backup, error)
        }
    }

    /// What restoring the Velero backup namespace/backup holds (os:admin): its namespaces, items
    /// and volumes, whether it can be restored, and the Restores of it already there. The same
    /// read follows a restore's progress.
    func veleroRestorePreview(namespace: String, backup: String) async throws -> VeleroRestorePreview {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeVeleroRestorePreview(config, context, kubeServer, namespace, backup, $0)
        }
    }

    /// Restores the Velero backup namespace/backup with options, like
    /// `velero restore create --from-backup` (os:admin): the Restore's name. Refused while the
    /// backup cannot be a source, its storage location is down, or a restore of it runs.
    func veleroRestore(namespace: String, backup: String, options: VeleroRestoreOptions) async throws -> String {
        let json = String(decoding: try JSONEncoder().encode(options), as: UTF8.self)
        return try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            IchorgoKubeVeleroRestore(config, context, kubeServer, namespace, backup, json, error)
        }
    }

    /// What the phone's own resolver answers for `records` (see DnsCheck): whether each hostname
    /// resolves, whether an answer is one of its targets, and who the ExternalDNS TXT registry
    /// record names. No cluster call: the records come from the app, the answers from the device.
    /// `controllers` give each record's owner id and TXT prefix and suffix; `demo` answers locally like the
    /// built-in cluster, never dialling.
    static func dnsCheck(_ records: [ExternalDnsRecord], controllers: [ExternalDnsController], demo: Bool) async throws -> DnsCheckResult {
        let owners = controllers.reduce(into: [String: ExternalDnsController]()) { $0[$1.name] = $1 }
        let requests = records.map { record in
            let controller = owners[record.controller]
            return DnsCheckRequest(dnsName: record.dnsName, recordType: record.recordType, targets: record.targets,
                                   ownerId: controller?.ownerId ?? "", txtPrefix: controller?.txtPrefix ?? "",
                                   txtSuffix: controller?.txtSuffix ?? "")
        }
        let json = try TalosJSON.encode(requests)
        return try await Self.json { demo ? IchorgoDemoDNSCheck(json, $0) : IchorgoDNSCheck(json, 0, $0) }
    }
}
