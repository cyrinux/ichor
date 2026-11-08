import Foundation
import Ichorgo
import IchorCore

/// Omni's machines and machine sets, and the join media with labels (see TalosClient for the conventions).
extension TalosClient {
    /// `nodeBootMedia` on Omni, with `labelsJSON` signed into the join token. Never cached.
    func nodeBootMediaOmni(arch: String, secureBoot: Bool, labelsJSON: String) async throws -> BootMedia {
        try await Self.json { [config, context] in
            IchorgoNodeBootMediaOmni(config, context, "", arch, secureBoot, labelsJSON, $0)
        }
    }

    /// Omni's join config with `labelsJSON` signed into the join token, and the cloud images. Never cached.
    func nodeCloudUserDataOmni(labelsJSON: String) async throws -> CloudUserData {
        try await Self.json { [config, context] in
            IchorgoNodeCloudUserDataOmni(config, context, "", labelsJSON, $0)
        }
    }

    /// The machine sets of the active cluster and how each takes machines.
    func omniMachineSets() async throws -> OmniMachineSets {
        try await Self.json { [config, context] in
            IchorgoOmniMachineSets(config, context, $0)
        }
    }

    /// The machines registered in Omni that no cluster has.
    func omniUnallocatedMachines() async throws -> [OmniMachine] {
        try await Self.json { [config, context] in
            IchorgoOmniUnallocatedMachines(config, context, $0)
        }
    }

    /// Puts a machine in the cluster and follows it (StartOmniAllocate). `stop` only stops following:
    /// the allocation stays. Go still reports `done`.
    func startOmniAllocate(_ spec: OmniAllocateSpec) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: JoinEvent.self)
        let bridge = JoinBridge(
            progress: { continuation.yield(.progress($0)) },
            done: { address, error in
                continuation.yield(.done(address: address, error: error))
                continuation.finish()
            }
        )
        let run = IchorgoStartOmniAllocate(config, context, spec.machineID, spec.machineSetID, bridge)
            .map(GoObject.init)
        continuation.onTermination = { _ in
            _ = bridge  // keep the listener alive for the whole run
            _ = run
        }
        return (stream, { run?.value.cancel() })
    }
}
