import Foundation
import Talosmobile
import TalosdevMobileCore

struct TalosError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

enum HealthEvent: Sendable {
    case progress(node: String, message: String)
    case done(error: String?)
}

enum SnapshotEvent: Sendable {
    case progress(bytes: Int64)
    case finished(path: String, size: Int64, sha256: String)
    case failed(String)
}

/// Swift face of the gomobile framework. Go calls block, so they run off the main actor.
/// Signatures come from the generated Talosmobile.objc.h (C functions with NSError**).
struct TalosClient: Sendable {
    let config: String
    let context: String

    static func parse(_ yaml: String) async throws -> ConfigSummary {
        try await json { TalosmobileParseConfig(yaml, $0) }
    }

    func overview() async throws -> ClusterOverview {
        try await Self.json { [config, context] in TalosmobileClusterOverview(config, context, $0) }
    }

    func services(node: String) async throws -> [ServiceInfo] {
        try await Self.json { [config, context] in TalosmobileNodeServices(config, context, node, $0) }
    }

    func resources(node: String) async throws -> NodeResources {
        try await Self.json { [config, context] in TalosmobileNodeResources(config, context, node, $0) }
    }

    /// Kernel log when `service` is nil.
    func logs(node: String, service: String?, lines: Int = 500) async throws -> LogTail {
        try await Self.json { [config, context] in
            if let service {
                return TalosmobileServiceLogs(config, context, node, service, lines, $0)
            }
            return TalosmobileKernelLogs(config, context, node, lines, $0)
        }
    }

    func kubespan() async throws -> KubeSpanOverview {
        try await Self.json { [config, context] in TalosmobileKubeSpanStatus(config, context, $0) }
    }

    /// Admin kubeconfig (os:admin). A credential: only write it where the user chose.
    func kubeconfig() async throws -> String {
        try await Self.run { [config, context] in TalosmobileKubeconfig(config, context, $0) }
    }

    /// Starts `talosctl debug` on node; events go to the listener (from Go threads).
    func startDebugShell(node: String, image: String, args: String, cols: Int, rows: Int,
                         listener: TalosmobileDebugListenerProtocol) -> TalosmobileDebugSession? {
        TalosmobileStartDebugShell(config, context, node, image, args, cols, rows, listener)
    }

    /// `talosctl -n NODE etcd defrag` (os:operator or os:admin); one member at a time.
    func defragment(node: String) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = TalosmobileEtcdDefragment(config, context, node, error)
        }
    }

    /// One sample of counters for the live graphs.
    func stats(node: String) async throws -> NodeStats {
        try await Self.json { [config, context] in TalosmobileNodeStats(config, context, node, $0) }
    }

    /// `talosctl processes` (os:reader); CPU time is cumulative, see processRows.
    func processes(node: String) async throws -> ProcessSample {
        try await Self.json { [config, context] in TalosmobileNodeProcesses(config, context, node, $0) }
    }

    /// Node's machine config as YAML (os:admin); secrets are masked unless revealSecrets.
    func machineConfig(node: String, revealSecrets: Bool) async throws -> String {
        try await Self.run { [config, context] in TalosmobileNodeMachineConfig(config, context, node, revealSecrets, $0) }
    }

    /// `talosctl etcd alarm disarm` through node (os:operator or os:admin).
    func disarmEtcdAlarms(node: String) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = TalosmobileEtcdAlarmDisarm(config, context, node, error)
        }
    }

    /// Streams `talosctl -n NODE etcd snapshot` into destPath; cancelling the consuming task
    /// cancels the transfer (Go then removes the partial file).
    func etcdSnapshot(node: String, destPath: String) -> AsyncStream<SnapshotEvent> {
        AsyncStream { continuation in
            let bridge = SnapshotBridge(
                progress: { continuation.yield(.progress(bytes: $0)) },
                done: {
                    continuation.yield($0)
                    continuation.finish()
                }
            )
            let run = TalosmobileStartEtcdSnapshot(config, context, node, destPath, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive for the whole transfer
            }
        }
    }

    func etcd() async throws -> EtcdOverview {
        try await Self.json { [config, context] in TalosmobileEtcdStatus(config, context, $0) }
    }

    func perform(_ request: PowerRequest, node: String) async throws {
        try await Self.run { [config, context] error -> Void in
            switch request.action {
            case .reboot: _ = TalosmobileReboot(config, context, node, request.rebootMode.cli, error)
            case .shutdown: _ = TalosmobileShutdown(config, context, node, request.forceShutdown, error)
            }
        }
    }

    /// Streams the server-side health check; cancelling the consuming task cancels the check.
    func health() -> AsyncStream<HealthEvent> {
        AsyncStream { continuation in
            let bridge = HealthBridge(
                progress: { continuation.yield(.progress(node: $0, message: $1)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = TalosmobileStartClusterHealth(config, context, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive for the whole check
            }
        }
    }

    private static func json<T: Decodable>(_ call: @escaping @Sendable (NSErrorPointer) -> String) async throws -> T {
        let output = try await run(call)
        return try TalosJSON.decode(T.self, from: output)
    }

    private static func run<T: Sendable>(_ call: @escaping @Sendable (NSErrorPointer) -> T) async throws -> T {
        try await Task.detached(priority: .userInitiated) {
            var error: NSError?
            let result = call(&error)
            if let error { throw TalosError(message: error.localizedDescription) }
            return result
        }.value
    }
}

/// gomobile exposes the Go interface as an Objective-C protocol; since a class with the same
/// name also exists, Swift imports the protocol as `…Protocol`.
private final class HealthBridge: NSObject, TalosmobileHealthListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (String, String) -> Void
    private let done: @Sendable (String?) -> Void

    init(progress: @escaping @Sendable (String, String) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ node: String?, message: String?) {
        progress(node ?? "", message ?? "")
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.flatMap { $0.isEmpty ? nil : $0 })
    }
}

private final class SnapshotBridge: NSObject, TalosmobileSnapshotListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (Int64) -> Void
    private let done: @Sendable (SnapshotEvent) -> Void

    init(progress: @escaping @Sendable (Int64) -> Void, done: @escaping @Sendable (SnapshotEvent) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ bytes: Int64) {
        progress(bytes)
    }

    func onDone(_ path: String?, size: Int64, sha256: String?, errMessage: String?) {
        if let errMessage, !errMessage.isEmpty {
            done(.failed(errMessage))
        } else {
            done(.finished(path: path ?? "", size: size, sha256: sha256 ?? ""))
        }
    }
}
