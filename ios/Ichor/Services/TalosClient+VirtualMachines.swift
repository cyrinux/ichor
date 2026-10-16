import Foundation
import IchorCore
import Ichorgo

/// KubeVirt (and Harvester) virtual machines and the actions on them, through the Kubernetes API (os:admin).
extension TalosClient {
    /// The VMs of `namespace`, every namespace when "".
    func virtualMachines(namespace: String = "") async throws -> VirtualMachines {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeVirtualMachines(config, context, kubeServer, namespace, $0)
        }
    }

    /// Runs `action` on `namespace`/`name`, like virtctl; the Go core refuses what the state forbids.
    func vmAction(namespace: String, name: String, action: VmAction) async throws -> VmActionResult {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeVMAction(config, context, kubeServer, namespace, name, action.rawValue, $0)
        }
    }

    /// Stops `namespace`/`name` with `graceSeconds` (0: at once, no grace period).
    func vmStopForce(namespace: String, name: String, graceSeconds: Int) async throws -> VmActionResult {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeVMStop(config, context, kubeServer, namespace, name, graceSeconds, $0)
        }
    }

    /// `virtctl console`: the serial console of the running VM `namespace`/`name`, raw bytes with
    /// no size to set. Events go to the listener (from Go threads).
    func startVMConsole(namespace: String, name: String, listener: IchorgoDebugListenerProtocol) -> IchorgoDebugSession? {
        IchorgoStartVMConsole(kubeConfig, kubeContext, kubeAPIServer, namespace, name, listener)
    }
}
