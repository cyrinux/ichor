import Foundation
import Security
import Talosmobile
import IchorCore

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

/// An imported context named like a stored one (see ImportConflicts in Go).
struct ImportConflict: Decodable, Sendable {
    let index: Int
    let suggested: String
    let sameAs: String?
}

/// UserDefaults keys of the screenshot mode.
enum PrivacyKeys {
    static let enabled = "privacyMask"
    static let words = "privacyMaskWords"
}

/// Swift face of the gomobile framework. Go calls block, so they run off the main actor.
/// Signatures come from the generated Talosmobile.objc.h (C functions with NSError**).
struct TalosClient: Sendable {
    let config: String
    let context: String
    /// The Kubernetes API address the user set for the cluster, "" for the kubeconfig's.
    var kubeServer = ""

    static func parse(_ yaml: String) async throws -> ConfigSummary {
        try await json { TalosmobileParseConfig(yaml, $0) }
    }

    /// A Kubernetes API address the user typed, as the https URL to store ("" when blank).
    static func normalizeKubeServer(_ input: String) async throws -> String {
        try await run { TalosmobileNormalizeKubeServer(input, $0) }
    }

    static func demoConfig() async throws -> String {
        try await run { TalosmobileDemoConfig($0) }
    }

    /// `stored` with context's ca/crt/key replaced by those of `generated` (a single-context
    /// talosconfig from generateTalosconfig); other contexts and fields are kept.
    static func replaceContextCredentials(stored: String, generated: String, context: String) async throws -> String {
        try await run { TalosmobileReplaceContextCredentials(stored, generated, context, $0) }
    }

    /// The contexts of `added` named like one of `stored`: the free name each gets, and the
    /// stored context of the same cluster (same CA) it may replace instead.
    static func importConflicts(stored: String, added: String) async throws -> [ImportConflict] {
        try await json { TalosmobileImportConflicts(stored, added, $0) }
    }

    /// `stored` with the contexts of `added` added: a context per cluster. A stored context is
    /// never overwritten: one of that name is added as name-1, unless `choices` (a JSON array
    /// of {index, name, replace}, see MergeConfig in Go) names it or replaces the same cluster.
    static func mergeConfig(stored: String, added: String, choices: String = "") async throws -> String {
        try await run { TalosmobileMergeConfig(stored, added, choices, $0) }
    }

    /// The backup file of `payload` (backup JSON) sealed with `passphrase` (Argon2id, AES-256-GCM).
    static func encryptBackup(payload: String, passphrase: String) async throws -> Data {
        try await run { TalosmobileEncryptBackup(payload, passphrase, $0) ?? Data() }
    }

    /// The validated payload JSON of a backup `file`.
    static func decryptBackup(_ file: Data, passphrase: String) async throws -> String {
        try await run { TalosmobileDecryptBackup(file, passphrase, $0) }
    }

    /// `stored` without `context` (not the last one: the stored config is deleted instead).
    static func removeContext(stored: String, context: String) async throws -> String {
        try await run { TalosmobileRemoveContext(stored, context, $0) }
    }

    /// `stored` with `nodes` (addresses) added to context's nodes; a context without nodes
    /// keeps its endpoints as its first nodes.
    static func addContextNodes(stored: String, context: String, nodes: [String]) async throws -> String {
        let list = nodes.joined(separator: ",")
        return try await run { TalosmobileAddContextNodes(stored, context, list, $0) }
    }

    /// Screenshot mode: Go masks IPs, hostnames, context names and `extraWords`
    /// (comma-separated) in everything it returns, and unmasks what it is given back.
    static func setPrivacyMask(enabled: Bool, extraWords: String) {
        TalosmobileSetPrivacyMask(enabled, extraWords)
    }

    /// Where Go remembers node names, so a node that is down still shows its hostname.
    /// Device-only: excluded from backup, encrypted with `dataKey()`. Call before any other Go call.
    static func setDataDirectory() {
        guard var url = try? FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                                     appropriateFor: nil, create: true)
            .appendingPathComponent("core", isDirectory: true),
            (try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)) != nil
        else { return }
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try? url.setResourceValues(values)
        TalosmobileSetDataDir(url.path, dataKey())
    }

    private static let dataKeyAccount = "core-data-key"

    /// The key Go encrypts what it remembers with: random, created once, in the Keychain. Readable
    /// after the first unlock, as background checks run while the phone is locked. Nil when the
    /// Keychain cannot be used (before the first unlock): nothing is remembered then.
    private static func dataKey() -> Data? {
        if let key = Keychain.read(dataKeyAccount), key.count == 32 { return key }
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else { return nil }
        let key = Data(bytes)
        // Fails while locked, so an existing key is never replaced by one that was only unreadable.
        guard (try? Keychain.write(key, account: dataKeyAccount,
                                   accessible: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly)) != nil else { return nil }
        return key
    }

    /// Applies the stored screenshot mode; call before any other Go call (also in background tasks).
    static func applyStoredPrivacyMask() {
        let defaults = UserDefaults.standard
        setPrivacyMask(enabled: defaults.bool(forKey: PrivacyKeys.enabled), extraWords: defaults.string(forKey: PrivacyKeys.words) ?? "")
    }

    func driftSnapshot() async throws -> String {
        try await Self.run { [config, context] in TalosmobileClusterDriftSnapshot(config, context, $0) }
    }
    func observation() async throws -> String {
        try await Self.run { [config, context] in TalosmobileClusterObservation(config, context, $0) }
    }
    static func compareDrift(baseline: String, current: String) async throws -> [DriftChange] {
        try await json { TalosmobileCompareDrift(baseline, current, $0) }
    }
    static func updateIncident(previous: String, observation: String, events: [NodeEvent]) async throws -> String {
        let encoded = String(decoding: try JSONEncoder().encode(events), as: UTF8.self)
        return try await run { TalosmobileUpdateIncident(previous, observation, encoded, $0) }
    }
    static func bottlenecks(previous: NodeStats, current: NodeStats) async throws -> Bottlenecks {
        let a = String(decoding: try JSONEncoder().encode(previous), as: UTF8.self)
        let b = String(decoding: try JSONEncoder().encode(current), as: UTF8.self)
        return try await json { TalosmobileCalculateBottlenecks(a, b, $0) }
    }

    func overview() async throws -> ClusterOverview {
        try await Self.json { [config, context] in TalosmobileClusterOverview(config, context, $0) }
    }

    /// The cluster's members (Talos cluster discovery), flagged when the context already targets them.
    func discoverNodes() async throws -> NodeDiscovery {
        try await Self.json { [config, context] in TalosmobileDiscoverNodes(config, context, $0) }
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
        try await Self.run { [config, context, kubeServer] in TalosmobileKubeconfig(config, context, kubeServer, $0) }
    }

    /// Deployments, StatefulSets and DaemonSets through the Kubernetes API (os:admin: Talos issues the kubeconfig).
    func workloads() async throws -> [KubeWorkload] {
        let list: KubeWorkloadList = try await Self.json { [config, context, kubeServer] in TalosmobileKubeWorkloads(config, context, kubeServer, $0) }
        return list.workloads
    }

    /// `kubectl rollout restart KIND/NAME -n NAMESPACE` (os:admin).
    func rolloutRestart(_ workload: KubeWorkload) async throws {
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = TalosmobileKubeRolloutRestart(config, context, kubeServer, workload.kind, workload.namespace, workload.name, error)
        }
    }

    /// The Ingress and HTTPRoute URLs serving pods (os:admin).
    func appRoutes(pods: [RoutePod]) async throws -> [KubeRoute] {
        let encoded = String(decoding: try JSONEncoder().encode(pods), as: UTF8.self)
        let list: KubeRouteList = try await Self.json { [config, context, kubeServer] in
            TalosmobileKubeAppRoutes(config, context, kubeServer, encoded, $0)
        }
        return list.routes
    }

    /// Every pod with the status `kubectl get pods` shows (os:admin).
    func pods() async throws -> [KubePod] {
        let list: KubePodList = try await Self.json { [config, context, kubeServer] in TalosmobileKubePods(config, context, kubeServer, $0) }
        return list.pods
    }

    /// `kubectl delete pod NAME -n NAMESPACE` (os:admin): its controller starts a new one.
    func deletePod(_ pod: KubePod) async throws {
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = TalosmobileKubeDeletePod(config, context, kubeServer, pod.namespace, pod.name, error)
        }
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

    /// The node's cgroup tree with pressure (os:admin: a copy of /sys/fs/cgroup); see cgroupRows.
    func cgroups(node: String) async throws -> CgroupReport {
        try await Self.json { [config, context] in TalosmobileNodeCgroups(config, context, node, $0) }
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

    /// Level, time, source, message and fields of a followed log line (Go ParseLogLine; an
    /// unparsed entry when it gives nothing usable). Blocking: call it off the main actor.
    static func parseLogLine(_ line: String) -> LogEntry {
        parseLogEntry(json: TalosmobileParseLogLine(line), line: line)
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

    /// The apps running in the cluster, from every node's containers (os:reader). One container
    /// listing per node: called when a screen opens or refreshes, not on a poll.
    func inventory() async throws -> ClusterInventory {
        try await Self.json { [config, context] in TalosmobileClusterInventory(config, context, $0) }
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

    static func json<T: Decodable>(_ call: @escaping @Sendable (NSErrorPointer) -> String) async throws -> T {
        let output = try await run(call)
        return try TalosJSON.decode(T.self, from: output)
    }

    static func run<T: Sendable>(_ call: @escaping @Sendable (NSErrorPointer) -> T) async throws -> T {
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

final class LogBridge: NSObject, TalosmobileLogListenerProtocol, @unchecked Sendable {
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
