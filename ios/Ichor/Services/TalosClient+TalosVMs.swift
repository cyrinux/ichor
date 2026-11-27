import Foundation
import IchorCore
import Ichorgo

/// The Talos hypervisor's virtual machines (Talos 1.15), through the Talos API (os:reader).
extension TalosClient {
    /// The VMs on `node`, their disks and its content libraries; `supported` is false with a
    /// reason on an older Talos.
    func nodeVirtualMachines(node: String) async throws -> TalosNodeVMs {
        try await Self.json { [config, context] in IchorgoNodeVirtualMachines(config, context, node, $0) }
    }

    /// `nodeVirtualMachines` for every node, in parallel; a node that does not answer carries an error.
    func clusterVirtualMachines() async throws -> TalosClusterVMs {
        try await Self.json { [config, context] in IchorgoClusterVirtualMachines(config, context, $0) }
    }
}
