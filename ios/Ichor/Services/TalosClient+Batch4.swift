import Foundation
import Talosmobile
import IchorCore

enum CaptureEvent: Sendable {
    case packets([PacketSummary])
    case stats(packets: Int64, bytes: Int64)
    case done(path: String, packets: Int64, bytes: Int64, error: String?)
}

enum UpgradeEvent: Sendable {
    case progress(UpgradeProgress)
    case done(newVersion: String, error: String?)
}

/// Packet capture and Talos upgrade calls (see TalosClient for the conventions).
extension TalosClient {
    /// Go's message for an invalid BPF expression, "" when it compiles. Blocking.
    static func validateCaptureFilter(_ expression: String) -> String {
        TalosmobileValidateCaptureFilter(expression)
    }

    /// `talosctl pcap` on node into destPath (os:operator or os:admin): stops after
    /// maxSeconds or maxBytes, or on `stop`. Go summarizes at most ~20 packets per second;
    /// they reach the stream in batches, 4 times a second. After a stop Go still reports `done`
    /// with the file, so stop rather than cancel the consuming task (which also stops the
    /// capture, but drops that last event).
    func packetCapture(node: String, options: CaptureOptions, destPath: String) -> (events: AsyncStream<CaptureEvent>, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: CaptureEvent.self)
        let bridge = CaptureBridge(
            packets: { continuation.yield(.packets($0)) },
            stats: { continuation.yield(.stats(packets: $0, bytes: $1)) },
            done: {
                continuation.yield($0)
                continuation.finish()
            }
        )
        let run = TalosmobileStartPacketCapture(config, context, node, options.interface, options.trimmedFilter,
                                                options.promiscuous, options.snapLen, options.duration.seconds,
                                                options.sizeLimit.bytes, destPath, bridge)
        let flusher = Task.detached {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 250_000_000)
                bridge.flush()
            }
        }
        continuation.onTermination = { _ in
            flusher.cancel()
            run?.cancel()
            _ = bridge // keep the listener alive for the whole capture
        }
        return (stream, { run?.cancel() })
    }

    /// Up to `limit` packet summaries of a capture file from `offset` (0-based).
    static func readPcap(path: String, offset: Int, limit: Int = pcapPageSize) async throws -> PcapPage {
        try await json { TalosmobileReadPcap(path, offset, limit, $0) }
    }

    /// Layers and hex dump of packet `index` (0-based) of a capture file.
    static func packetDetail(path: String, index: Int) async throws -> PacketDetail {
        try await json { TalosmobilePacketDetail(path, index, $0) }
    }

    /// Current version, installer image and what blocks an upgrade of node (os:admin).
    func upgradePlan(node: String) async throws -> UpgradePlan {
        try await Self.json { [config, context] in TalosmobileUpgradePlan(config, context, node, $0) }
    }

    /// Recent Talos releases from GitHub, newest first.
    static func talosReleases() async throws -> [TalosRelease] {
        try await json { TalosmobileTalosReleases($0) }
    }

    /// Latest stable Talos against the given node versions (GitHub, no node call).
    static func talosUpdateCheck(versionsCSV: String) async throws -> TalosUpdateInfo {
        try await json { TalosmobileTalosUpdateCheck(versionsCSV, $0) }
    }

    /// The installer image for `version` keeping currentImage's registry and schematic.
    static func upgradeImage(currentImage: String, version: String) async -> String {
        await Task.detached(priority: .userInitiated) { TalosmobileUpgradeImage(currentImage, version) }.value
    }

    /// `talosctl upgrade` and following the node until it is back (os:admin). Cancelling the
    /// consuming task only stops following: the node keeps upgrading.
    func upgrade(node: String, image: String, stage: Bool, force: Bool) -> AsyncStream<UpgradeEvent> {
        AsyncStream { continuation in
            let bridge = UpgradeBridge(
                progress: { continuation.yield(.progress($0)) },
                done: {
                    continuation.yield(.done(newVersion: $0, error: $1))
                    continuation.finish()
                }
            )
            let run = TalosmobileStartUpgrade(config, context, node, image, stage, force, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive while following
            }
        }
    }
}

/// Decodes packets on the Go thread and hands them over in batches (see flush).
private final class CaptureBridge: NSObject, TalosmobileCaptureListenerProtocol, @unchecked Sendable {
    private let packets: @Sendable ([PacketSummary]) -> Void
    private let stats: @Sendable (Int64, Int64) -> Void
    private let done: @Sendable (CaptureEvent) -> Void
    private let lock = NSLock()
    private var pending: [PacketSummary] = []

    init(packets: @escaping @Sendable ([PacketSummary]) -> Void, stats: @escaping @Sendable (Int64, Int64) -> Void,
         done: @escaping @Sendable (CaptureEvent) -> Void) {
        self.packets = packets
        self.stats = stats
        self.done = done
    }

    /// Yields under the lock, so a last batch never lands after onDone finished the stream.
    func flush() {
        lock.lock()
        defer { lock.unlock() }
        guard !pending.isEmpty else { return }
        packets(pending)
        pending = []
    }

    func onPacket(_ summaryJSON: String?) {
        guard let summaryJSON, let packet = try? TalosJSON.decode(PacketSummary.self, from: summaryJSON) else { return }
        lock.lock()
        pending.append(packet)
        // A burst between flushes: the screen only shows the newest rows anyway.
        if pending.count > liveCaptureRows { pending.removeFirst(pending.count - liveCaptureRows) }
        lock.unlock()
    }

    func onStats(_ packets: Int64, bytes: Int64) {
        stats(packets, bytes)
    }

    func onDone(_ path: String?, packets: Int64, bytes: Int64, errMessage: String?) {
        flush()
        done(.done(path: path ?? "", packets: packets, bytes: bytes, error: errMessage.flatMap { $0.isEmpty ? nil : $0 }))
    }
}

private final class UpgradeBridge: NSObject, TalosmobileUpgradeListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (UpgradeProgress) -> Void
    private let done: @Sendable (String, String?) -> Void

    init(progress: @escaping @Sendable (UpgradeProgress) -> Void, done: @escaping @Sendable (String, String?) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(UpgradeProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ newVersion: String?, errMessage: String?) {
        done(newVersion ?? "", errMessage.flatMap { $0.isEmpty ? nil : $0 })
    }
}
