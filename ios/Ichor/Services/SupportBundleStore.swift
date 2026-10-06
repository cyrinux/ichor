import Foundation
import IchorCore
import UIKit

/// Support bundles in Application Support/support, excluded from backups (they hold logs and
/// cluster details, and are easy to recreate).
enum SupportBundleStore {
    static func directory() throws -> URL { try AppSupport.excludedFolder("support") }

    /// Newest first; only files this app wrote.
    static func list() throws -> [SupportBundleFile] {
        try sortSupportBundles(LocalFileStore.list(in: directory()).filter { isSupportBundleName($0.name) })
    }

    static func delete(_ url: URL) throws {
        try LocalFileStore.delete(url)
    }

    /// Removes what a collection killed with the app left behind (Go writes NAME.part, then
    /// renames); called at launch, when no collection can be running.
    static func removeStaleParts() {
        guard let folder = try? directory(),
              let urls = try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil) else { return }
        for url in urls where isStaleSupportPartName(url.lastPathComponent) {
            try? FileManager.default.removeItem(at: url)
        }
    }
}

/// One support bundle collection, owned by its screen; cancelled when the screen goes away.
@Observable
@MainActor
final class SupportBundleJob {
    enum State: Equatable {
        case idle
        case running
        case finished(file: URL, size: Int64)
        case failed(String)
        case cancelled
    }

    private(set) var state = State.idle
    /// The selected nodes (addresses), in the cluster's order.
    private(set) var nodes: [String] = []
    private(set) var progress = SupportBundleProgress()
    private var task: Task<Void, Never>?

    var isRunning: Bool { state == .running }

    func start(client: TalosClient, context: String, nodes selected: [String]) {
        guard !isRunning, !selected.isEmpty else { return }
        let url: URL
        do {
            url = try SupportBundleStore.directory().appendingPathComponent(supportBundleFilename(context: context, date: Date()))
        } catch {
            state = .failed(error.localizedDescription)
            return
        }
        nodes = selected
        progress = SupportBundleProgress()
        state = .running
        UIApplication.shared.isIdleTimerDisabled = true
        task = Task {
            var outcome: State?
            for await event in client.supportBundle(nodesCSV: selected.joined(separator: ","), destPath: url.path) {
                switch event {
                case .progress(let step):
                    progress = progress.recording(step)
                case .done(let path, let size, let error):
                    if let error {
                        outcome = .failed(error)
                    } else {
                        outcome = .finished(file: path.isEmpty ? url : URL(fileURLWithPath: path), size: size)
                    }
                }
            }
            finish(Task.isCancelled ? nil : outcome, partial: url)
        }
    }

    func cancel() { task?.cancel() }

    /// Back to the node selection.
    func reset() {
        guard !isRunning else { return }
        state = .idle
        progress = SupportBundleProgress()
    }

    private func finish(_ outcome: State?, partial: URL) {
        task = nil
        let result = outcome ?? .cancelled
        // Cancelled or failed: no half-written archive is kept.
        if case .finished = result {} else {
            try? FileManager.default.removeItem(at: partial)
            try? FileManager.default.removeItem(at: partial.appendingPathExtension("part"))
        }
        state = result
        // A followed upgrade or a node maintenance keeps the screen on too.
        UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
    }
}
