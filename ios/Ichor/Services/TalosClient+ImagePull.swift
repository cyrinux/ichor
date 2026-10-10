import Foundation
import Ichorgo
import IchorCore

enum ImagePullEvent: Sendable {
    case progress(ImagePullProgress)
    /// nil when every node pulled the image.
    case done(error: String?)
}

/// Pulling an image on several nodes ahead of an upgrade (StartImagePull).
extension TalosClient {
    /// Pulls `image` into `namespace` on `nodes` (every node of the context when empty), three
    /// at a time. Ending the stream cancels the pulls in flight.
    func imagePull(nodes: [String], image: String, namespace: ImagePullNamespace) -> AsyncStream<ImagePullEvent> {
        // Each event carries every node's state: the latest is enough.
        Self.bridged(buffering: .bufferingNewest(1)) { [config, context] continuation in
            let bridge = ImagePullBridge(
                progress: { continuation.yield(.progress($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartImagePull(config, context, nodes.joined(separator: ","), image, namespace.rawValue, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

private final class ImagePullBridge: NSObject, IchorgoImagePullListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<ImagePullProgress>

    init(progress: @escaping @Sendable (ImagePullProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}
