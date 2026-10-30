import Foundation
import Ichorgo
import IchorCore

enum JoinEvent: Sendable {
    case progress(JoinProgress)
    /// `address` the joined node's ("" on failure); `error` nil on success.
    case done(address: String, error: String?)
}

/// Adding a node to the cluster (see TalosClient for the conventions).
extension TalosClient {
    /// A node in maintenance mode at `address` (IP or host name on the LAN or VPN): version, disks
    /// and links over the unauthenticated maintenance API. Static: the node belongs to no cluster
    /// yet, so no talosconfig is involved.
    static func maintenanceNodeInspect(address: String) async throws -> MaintenanceInspection {
        try await json { IchorgoMaintenanceNodeInspect(address, 0, $0) }
    }

    /// A new node's config from `template`'s (os:admin: read with its secrets), redacted. The
    /// role must be the template's; `staticAddress` is a prefix or "" for DHCP.
    func joinNodeConfigPreview(
        template: String, role: String, hostname: String, installDisk: String,
        staticAddress: String
    ) async throws -> JoinConfigPreview {
        try await Self.json { [config, context] in
            IchorgoJoinNodeConfigPreview(config, context, template, role, hostname, installDisk, staticAddress, "", $0)
        }
    }

    /// What to confirm before joining a node of `role`, one warning per line.
    static func nodeJoinWarnings(role: String) async throws -> [String] {
        let text = try await run { IchorgoNodeJoinWarnings(role, $0) }
        return text.split(separator: "\n").map(String.init).filter { !$0.isEmpty }
    }

    /// Sends the new node's config (made from the template's) to the node in maintenance mode,
    /// then follows it until it joins (StartNodeJoin). After `stop` Go still reports `done`:
    /// once the config is sent the install goes on, only the following stops.
    func startNodeJoin(_ request: JoinRequest) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: JoinEvent.self)
        let bridge = JoinBridge(
            progress: { continuation.yield(.progress($0)) },
            done: { address, error in
                continuation.yield(.done(address: address, error: error))
                continuation.finish()
            }
        )
        let started = IchorgoStartNodeJoin(
            config, context, request.address, request.template, request.role, request.hostname,
            request.disk, request.staticAddress, "", request.replacing, bridge)
        let run = started.map(GoObject.init)
        continuation.onTermination = { _ in
            run?.value.cancel()
            _ = bridge  // keep the listener alive for the whole run
        }
        return (stream, { run?.value.cancel() })
    }
}

private final class JoinBridge: NSObject, IchorgoJoinListenerProtocol, Sendable {
    private let progress: @Sendable (JoinProgress) -> Void
    private let done: @Sendable (String, String?) -> Void

    init(progress: @escaping @Sendable (JoinProgress) -> Void, done: @escaping @Sendable (String, String?) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let event = try? TalosJSON.decode(JoinProgress.self, from: json) else { return }
        progress(event)
    }

    func onDone(_ nodeAddress: String?, errMessage: String?) {
        done(nodeAddress ?? "", errMessage.nonEmpty)
    }
}
