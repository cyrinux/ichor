import Foundation
import Ichorgo
import IchorCore

enum ImageScanEvent: Sendable {
    case progress(ImageScanProgress)
    /// `json` is the core's report as is (the exports are written from it); `error` is nil when
    /// the scan completed.
    case done(ImageScanReport?, json: String, error: String?)
}

/// Vulnerability scans of an app's images through the Kubernetes API (os:admin): a Trivy Job in
/// the ichor-imagescan namespace, or the Trivy Operator's reports (see TalosClient for the
/// conventions).
extension TalosClient {
    /// The Trivy Operator's reports on the pods' images; `available` false without it.
    func imageScanOperatorReports(pods: [RoutePod]) async throws -> OperatorReports {
        let encoded = try TalosJSON.encode(pods)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoImageScanOperatorReports(config, context, kubeServer, encoded, $0)
        }
    }

    /// Starts a scan of the pods' images and of `images`, refs with no pod behind them (a node's
    /// system images). The stream finishes after `done`; `stop` ends it early, and `done` follows
    /// once Go deleted the scan Job.
    func imageScan(pods: [RoutePod], images: [String] = []) throws -> (events: AsyncStream<ImageScanEvent>, stop: @Sendable () -> Void) {
        let encoded = try TalosJSON.encode(pods)
        let options = try imageScanOptions(images: images)
        let (stream, continuation) = AsyncStream.makeStream(of: ImageScanEvent.self)
        let bridge = ImageScanBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done($0, json: $1, error: $2))
                continuation.finish()
            }
        )
        let run = IchorgoStartImageScan(kubeConfig, kubeContext, kubeAPIServer, encoded, options, bridge)
        continuation.onTermination = { _ in
            run?.cancel()
            _ = bridge // keep the listener alive for the whole scan
        }
        return (stream, { run?.cancel() })
    }

    /// `reportJSON` (a scan's or the operator's) written in `format`.
    static func imageScanExport(_ reportJSON: String, format: ImageScanFormat) async throws -> String {
        try await run { IchorgoImageScanExport(reportJSON, format.rawValue, $0) }
    }
}

private final class ImageScanBridge: NSObject, IchorgoImageScanListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (ImageScanProgress) -> Void
    private let done: @Sendable (ImageScanReport?, String, String?) -> Void

    init(progress: @escaping @Sendable (ImageScanProgress) -> Void,
         done: @escaping @Sendable (ImageScanReport?, String, String?) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(ImageScanProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ reportJSON: String?, errMessage: String?) {
        let json = reportJSON ?? ""
        done(try? TalosJSON.decode(ImageScanReport.self, from: json), json, errMessage.nonEmpty)
    }
}
