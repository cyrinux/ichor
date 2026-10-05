import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// Health of Longhorn, Garage and CloudNativePG (os:admin). hints: their catalog ids seen in the
    /// inventory (see dataServiceHints); "" checks everything.
    func dataServices(hints: String) async throws -> DataServices {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeDataServices(config, context, kubeServer, hints, $0) }
    }

    /// What the blocks failing to resync in a Garage cluster are, from a sample (os:admin).
    func garageBlockErrors(_ instance: GarageInstance) async throws -> GarageBlockReport {
        let (namespace, pod) = (instance.namespace, instance.pod)
        return try await Self.json { [config, context, kubeServer] in
            IchorgoKubeGarageBlockErrors(config, context, kubeServer, namespace, pod, $0)
        }
    }

    /// Launches the safe repairs for the blocks failing to resync (os:admin); they run asynchronously.
    func garageRepairBlocks(_ instance: GarageInstance) async throws -> GarageRepairResult {
        let (namespace, pod) = (instance.namespace, instance.pod)
        return try await Self.json { [config, context, kubeServer] in
            IchorgoKubeGarageRepairBlocks(config, context, kubeServer, namespace, pod, $0)
        }
    }

    /// Sets the resync tranquility of a Garage node, full id or "*" (os:admin): 0 is full speed, 2 the default.
    func garageSetTranquility(_ instance: GarageInstance, node: String, value: Int) async throws {
        let (namespace, pod) = (instance.namespace, instance.pod)
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoKubeGarageSetTranquility(config, context, kubeServer, namespace, pod, node, value, error)
        }
    }

    /// Runs action on the Longhorn volume or node namespace/name (os:admin); namespace is Longhorn's
    /// own. value: the replica count for .replicas, ignored otherwise.
    func longhornAction(namespace: String, name: String, action: LonghornAction, value: Int = 0) async throws {
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoKubeLonghornAction(config, context, kubeServer, namespace, name, action.rawValue, value, error)
        }
    }

    /// What explains the state of the cert-manager certificate namespace/name (os:admin): its
    /// requests, ACME orders and challenges, their events and controller log lines.
    func certificateDetails(namespace: String, name: String) async throws -> CertDetails {
        try await Self.json { [config, context, kubeServer] in
            IchorgoKubeCertManagerDetails(config, context, kubeServer, namespace, name, $0)
        }
    }

    /// Issues the cert-manager certificate namespace/name again now, like `cmctl renew` (os:admin).
    /// Refused while it is already being issued.
    func renewCertificate(namespace: String, name: String) async throws {
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoKubeCertManagerRenew(config, context, kubeServer, namespace, name, error)
        }
    }
}
