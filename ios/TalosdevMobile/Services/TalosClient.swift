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

enum EventStreamItem: Sendable {
    case event(NodeEvent)
    case done(error: String?)
}

enum LogFollowItem: Sendable {
    case line(String)
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

    /// `stored` with context's ca/crt/key replaced by those of `generated` (a single-context
    /// talosconfig from generateTalosconfig); other contexts and fields are kept.
    static func replaceContextCredentials(stored: String, generated: String, context: String) async throws -> String {
        try await run { TalosmobileReplaceContextCredentials(stored, generated, context, $0) }
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

    /// `talosctl events` from nodes (nil = the context's nodes), replaying the last `tail` per
    /// node first (os:reader); cancelling the consuming task cancels the stream.
    func events(node: String?, tail: Int = 50) -> AsyncStream<EventStreamItem> {
        AsyncStream { continuation in
            let bridge = EventsBridge(
                event: { continuation.yield(.event($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = TalosmobileStartEvents(config, context, node ?? "", tail, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive for the whole stream
            }
        }
    }

    /// `talosctl logs -f` (kernel log when `service` is nil), starting with the last `tailLines`.
    func followLogs(node: String, service: String?, tailLines: Int = 200) -> AsyncStream<LogFollowItem> {
        AsyncStream { continuation in
            let bridge = LogBridge(
                line: { continuation.yield(.line($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = TalosmobileStartLogFollow(config, context, node, service ?? "", tailLines, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive for the whole stream
            }
        }
    }

    /// Kubernetes containers on node with cumulative CPU time (os:reader), see containerRows.
    func containers(node: String) async throws -> ContainerSample {
        try await Self.json { [config, context] in TalosmobileNodeContainers(config, context, node, $0) }
    }

    /// `talosctl service SERVICE start|stop|restart` (os:operator or os:admin).
    func serviceAction(_ action: ServiceAction, service: String, node: String) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = TalosmobileServiceAction(config, context, node, service, action.rawValue, error)
        }
    }

    /// A new client certificate as a single-context talosconfig (os:admin): roles is the
    /// comma-separated list, see rolesArgument. A credential: keep it in memory only.
    func generateTalosconfig(roles: String, hours: Int) async throws -> String {
        try await Self.run { [config, context] in TalosmobileGenerateTalosconfig(config, context, roles, hours, $0) }
    }

    /// Links, addresses, routes, DNS and time servers of node (os:reader).
    func network(node: String) async throws -> NodeNetwork {
        try await Self.json { [config, context] in TalosmobileNodeNetwork(config, context, node, $0) }
    }

    /// `talosctl netstat -a -p` on node (os:reader).
    func connections(node: String) async throws -> [NodeConnection] {
        try await Self.json { [config, context] in TalosmobileNodeConnections(config, context, node, $0) }
    }

    /// Node clock against its NTP server (os:reader).
    func nodeTime(node: String) async throws -> NodeTimeInfo {
        try await Self.json { [config, context] in TalosmobileNodeTime(config, context, node, $0) }
    }

    /// Clock offset of every node of the context; failures are reported per node (os:reader).
    func clusterTime() async throws -> ClusterTimeInfo {
        try await Self.json { [config, context] in TalosmobileClusterTime(config, context, $0) }
    }

    /// System, CPUs, memory, disks, extensions and security state of node (os:reader).
    func hardware(node: String) async throws -> NodeHardware {
        try await Self.json { [config, context] in TalosmobileNodeHardware(config, context, node, $0) }
    }

    /// Container images in node's CRI namespace (os:reader).
    func images(node: String) async throws -> [ContainerImage] {
        try await Self.json { [config, context] in TalosmobileNodeImages(config, context, node, $0) }
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

private final class EventsBridge: NSObject, TalosmobileEventListenerProtocol, @unchecked Sendable {
    private let event: @Sendable (NodeEvent) -> Void
    private let done: @Sendable (String?) -> Void

    init(event: @escaping @Sendable (NodeEvent) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.event = event
        self.done = done
    }

    func onEvent(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(NodeEvent.self, from: json) else { return }
        event(decoded)
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.flatMap { $0.isEmpty ? nil : $0 })
    }
}

private final class LogBridge: NSObject, TalosmobileLogListenerProtocol, @unchecked Sendable {
    private let line: @Sendable (String) -> Void
    private let done: @Sendable (String?) -> Void

    init(line: @escaping @Sendable (String) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.line = line
        self.done = done
    }

    func onLine(_ line: String?) {
        self.line(line ?? "")
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.flatMap { $0.isEmpty ? nil : $0 })
    }
}
