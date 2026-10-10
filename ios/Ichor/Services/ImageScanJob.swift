import Foundation
import IchorCore

/// The vulnerability scan of an app's images running or last run, app-wide and one at a time:
/// it goes on while the user leaves the app's sheet or the Apps screen. The scan Job deletes
/// itself in the cluster if the app is killed meanwhile.
@Observable
@MainActor
final class ImageScanJob {
    static let shared = ImageScanJob()

    private(set) var state: ImageScanState?
    private var stopScan: (@Sendable () -> Void)?

    var isRunning: Bool { state?.running == true }

    /// The scan of `appID` in `context`, nil when the last one was of another app or cluster.
    func scan(context: String, appID: String) -> ImageScanState? {
        guard let state, state.context == context, state.appID == appID else { return nil }
        return state
    }

    /// Another app's (or cluster's) scan runs.
    func busy(otherThan context: String, appID: String) -> Bool {
        guard let state, state.running else { return false }
        return state.context != context || state.appID != appID
    }

    /// Scans `pods`' images and the `images` refs (a node's system images), replacing the last
    /// scan. Ignored while one runs.
    func start(client: TalosClient, context: String, appID: String, pods: [RoutePod], images: [String] = []) {
        guard !isRunning else { return }
        var started = ImageScanState(context: context, appID: appID)
        let scan: (events: AsyncStream<ImageScanEvent>, stop: @Sendable () -> Void)
        do {
            scan = try client.imageScan(pods: pods, images: images)
        } catch {
            started.finish(report: nil, json: "", error: error.localizedDescription)
            state = started
            return
        }
        // The handle to stop it is kept at once, and dropped once Go reported the scan done.
        stopScan = scan.stop
        state = started
        Task {
            for await event in scan.events {
                switch event {
                case .progress(let progress):
                    state?.progress = progress
                case .done(let report, let json, let error):
                    state?.finish(report: report, json: json, error: error)
                }
            }
            stopScan = nil
        }
    }

    /// Ends the scan early; Go deletes its Job, then reports it done.
    func stop() {
        guard isRunning else { return }
        state?.stopping = true
        stopScan?()
    }
}
