import Foundation
import Ichorgo
import IchorCore

/// Pulling a model onto an Ollama server of the cluster (StartOllamaPull).
extension TalosClient {
    /// Pulls `model` onto the Ollama server `namespace`/`server` (its workload, as the plugin's
    /// server rows name it): the progress, then the end of the stream once the model is on the
    /// server, or Ollama's (or the API server's) error. Ending the stream (its task cancelled)
    /// stops the download; the layers already there stay and a new pull resumes from them.
    func ollamaPull(
        namespace: String, server: String, model: String
    ) -> AsyncThrowingStream<OllamaPullProgress, Error> {
        Self.bridgedThrowing { [config = kubeConfig, context = kubeContext, kubeServer = kubeAPIServer] continuation in
            let bridge = OllamaPullBridge(
                progress: { continuation.yield($0) },
                done: { error in
                    if let error {
                        continuation.finish(throwing: TalosError(message: error))
                    } else {
                        continuation.finish()
                    }
                }
            )
            let run = IchorgoStartOllamaPull(config, context, kubeServer, namespace, server, model, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

private final class OllamaPullBridge: NSObject, IchorgoOllamaPullListenerProtocol, Sendable {
    private let progress: @Sendable (OllamaPullProgress) -> Void
    private let done: @Sendable (String?) -> Void

    init(progress: @escaping @Sendable (OllamaPullProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ status: String?, completed: Int64, total: Int64) {
        progress(OllamaPullProgress(status: status ?? "", completed: completed, total: total))
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
