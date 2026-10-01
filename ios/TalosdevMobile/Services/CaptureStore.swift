import Foundation
import TalosdevMobileCore
import UIKit

/// Capture files in Application Support/captures, excluded from backups (they can hold
/// sensitive traffic and are easy to recreate).
enum CaptureStore {
    static func directory() throws -> URL {
        var url = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                              appropriateFor: nil, create: true)
            .appendingPathComponent("captures", isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try url.setResourceValues(values)
        return url
    }

    static func url(of file: CaptureFile) throws -> URL {
        try directory().appendingPathComponent(file.name)
    }

    /// Newest first.
    static func list() throws -> [CaptureFile] {
        let keys: [URLResourceKey] = [.fileSizeKey, .contentModificationDateKey]
        let urls = try FileManager.default.contentsOfDirectory(at: directory(), includingPropertiesForKeys: keys)
        let files = urls.map { url in
            let values = try? url.resourceValues(forKeys: Set(keys))
            return CaptureFile(name: url.lastPathComponent, size: Int64(values?.fileSize ?? 0),
                               modified: values?.contentModificationDate ?? .distantPast)
        }
        return sortCaptureFiles(files)
    }

    static func delete(_ url: URL) throws {
        try FileManager.default.removeItem(at: url)
    }
}

/// One packet capture, owned by its screen; Go events hop to the main actor in batches.
@Observable
@MainActor
final class CaptureSession {
    enum State: Equatable {
        case setup
        case running
        /// `error` set when Go stopped on a failure; the file may still hold packets.
        case finished(file: URL?, error: String?)
    }

    private(set) var state = State.setup
    private(set) var packets: [PacketSummary] = []
    private(set) var packetCount: Int64 = 0
    private(set) var bytes: Int64 = 0
    private(set) var startedAt: Date?
    private(set) var stoppedAt: Date?
    private var task: Task<Void, Never>?
    private var stopCapture: (@Sendable () -> Void)?

    var isRunning: Bool { state == .running }

    func start(client: TalosClient, node: String, hostname: String, options: CaptureOptions) {
        guard !isRunning else { return }
        let url: URL
        do {
            url = try CaptureStore.directory()
                .appendingPathComponent(captureFilename(hostname: hostname, interface: options.interface, date: Date()))
        } catch {
            state = .finished(file: nil, error: error.localizedDescription)
            return
        }
        packets = []
        packetCount = 0
        bytes = 0
        startedAt = Date()
        stoppedAt = nil
        state = .running
        UIApplication.shared.isIdleTimerDisabled = true
        let capture = client.packetCapture(node: node, options: options, destPath: url.path)
        stopCapture = capture.stop
        task = Task {
            var outcome: State?
            for await event in capture.events {
                switch event {
                case .packets(let batch):
                    packets = appendPackets(packets, batch)
                case .stats(let count, let size):
                    packetCount = count
                    bytes = size
                case .done(let path, let count, let size, let error):
                    packetCount = count
                    bytes = size
                    let file = path.isEmpty ? url : URL(fileURLWithPath: path)
                    outcome = .finished(file: FileManager.default.fileExists(atPath: file.path) ? file : nil, error: error)
                }
            }
            finish(outcome ?? .finished(file: FileManager.default.fileExists(atPath: url.path) ? url : nil, error: nil))
        }
    }

    /// Stops the capture; the file keeps what was captured so far.
    func stop() { stopCapture?() }

    /// The screen went away: stop at once without waiting for Go's report.
    func leave() {
        stopCapture?()
        task?.cancel()
    }

    func reset() {
        leave()
        state = .setup
        packets = []
    }

    private func finish(_ outcome: State) {
        task = nil
        stopCapture = nil
        stoppedAt = Date()
        state = outcome
        // A followed upgrade keeps the screen on too.
        UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive
    }
}
