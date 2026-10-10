import Foundation
import IchorCore
import UIKit

/// The image pull running or last run, app-wide and one at a time: it goes on while the user
/// leaves the sheet, the Images screen or the upgrade screen. A background task keeps it going
/// a little while once the app leaves the screen.
@Observable
@MainActor
final class ImagePullJob {
    static let shared = ImagePullJob()

    enum Outcome: Equatable {
        case succeeded
        case stopped
        case failed(String)
    }

    private(set) var image = ""
    private(set) var namespace = ImagePullNamespace.system
    private(set) var progress = ImagePullProgress()
    private(set) var outcome: Outcome?
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    var isRunning: Bool { task != nil }
    /// A pull ran or runs: the sheet has something to show.
    var hasRun: Bool { !image.isEmpty }

    /// Pulls `image` on `nodes` (every node when empty). Ignored while a pull runs.
    func start(client: TalosClient, image: String, namespace: ImagePullNamespace, nodes: [String] = []) {
        guard !isRunning else { return }
        self.image = image.trimmingCharacters(in: .whitespacesAndNewlines)
        self.namespace = namespace
        progress = ImagePullProgress()
        outcome = nil
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Image pull") {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.endBackgroundTask() }
        }
        let events = client.imagePull(nodes: nodes, image: self.image, namespace: namespace)
        task = Task {
            var result = Outcome.stopped
            for await event in events {
                switch event {
                case .progress(let p): progress = p
                case .done(let error): result = error.map { .failed($0) } ?? .succeeded
                }
            }
            if outcome == nil { outcome = result }
            task = nil
            endBackgroundTask()
        }
    }

    /// Cancels the pulls in flight; the nodes not reached stay pending.
    func stop() {
        guard isRunning else { return }
        outcome = .stopped
        task?.cancel()
    }

    /// Forgets a finished pull.
    func clear() {
        guard !isRunning else { return }
        image = ""
        progress = ImagePullProgress()
        outcome = nil
    }

    /// Ends the background task once, whoever comes first (pull over, or expiry).
    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }
}
