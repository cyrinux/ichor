import Foundation
import Ichorgo
import IchorCore

/// Creating a server at Hetzner for a new node (see TalosClient for the conventions).
extension TalosClient {
    /// What a Hetzner token may do. Creates nothing.
    static func hetznerTest(token: String) async throws -> HetznerTokenCheck {
        let tokenJSON = hetznerTokenJSON(token)
        return try await json { IchorgoHetznerTest(tokenJSON, $0) }
    }

    /// Server types, locations and snapshots (`token` may be empty on the demo). Never cached.
    func hetznerOptions(token: String) async throws -> HetznerOptions {
        let tokenJSON = Self.hetznerTokenJSON(token)
        return try await Self.json { [config, context] in
            IchorgoHetznerOptions(config, context, tokenJSON, $0)
        }
    }

    /// Creates the server with the new node's user data and follows it (StartHetznerCreate). `stop`
    /// deletes the server; Go still reports `done`. The stream going away does not stop the run:
    /// that would delete a billable server the person did not ask to delete.
    func startHetznerCreate(
        _ spec: HetznerCreateSpec, token: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        let tokenJSON = Self.hetznerTokenJSON(token)
        return hetznerRun { [config, context] bridge in
            IchorgoStartHetznerCreate(config, context, tokenJSON, specJSON, bridge)
        }
    }

    /// The cost line of a snapshot run. Creates nothing.
    static func hetznerSnapshotEstimate(token: String, arch: String, location: String) async throws
        -> HetznerSnapshotCost
    {
        let tokenJSON = hetznerTokenJSON(token)
        return try await json { IchorgoHetznerSnapshotEstimate(tokenJSON, arch, location, $0) }
    }

    /// Makes a Talos snapshot with a throwaway server (StartHetznerSnapshot): `done`'s address is the
    /// result JSON. `stop` deletes what the run created, and so only `stop` stops it, as above.
    func startHetznerSnapshot(
        _ spec: HetznerSnapshotSpec, token: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        let tokenJSON = Self.hetznerTokenJSON(token)
        return hetznerRun { [config, context] bridge in
            IchorgoStartHetznerSnapshot(config, context, tokenJSON, specJSON, bridge)
        }
    }

    private func hetznerRun(
        _ start: (JoinBridge) -> IchorgoJoinRun?
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: JoinEvent.self)
        let bridge = JoinBridge(
            progress: { continuation.yield(.progress($0)) },
            done: { address, error in
                continuation.yield(.done(address: address, error: error))
                continuation.finish()
            }
        )
        let run = start(bridge).map(GoObject.init)
        continuation.onTermination = { _ in
            _ = bridge  // keep the listener alive for the whole run
            _ = run
        }
        return (stream, { run?.value.cancel() })
    }

    private static func hetznerTokenJSON(_ token: String) -> String {
        (try? JSONEncoder().encode(["token": token])).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
    }
}
