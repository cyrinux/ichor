import Foundation
import Ichorgo
import IchorCore

enum PromChatEvent: Sendable {
    /// The whole explanation so far, not only what is new (never the panel block).
    case answer(String)
    /// `panel` is what the model proposed, checked by Go; nil for a plain answer.
    case done(panel: PanelSuggestion?, error: String?)
}

/// The panel assistant calls (see TalosClient for the conventions).
extension TalosClient {
    /// The metric names `source` knows, for the model to use metrics that exist.
    func promMetricNames(_ source: PromSource) async throws -> [String] {
        let sourceJSON = try TalosJSON.encode(source)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoPromMetricNames(config, context, kubeServer, sourceJSON, $0)
        }
    }

    /// A new conversation about `source`; `current` is the panel being edited, nil for a new one.
    func newPromChat(_ source: PromSource, metricNames: [String], current: PromPanel?) async throws -> PromChatSession {
        let sourceJSON = try TalosJSON.encode(source)
        let namesJSON = try TalosJSON.encode(metricNames)
        let edited = current.flatMap { $0.query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : $0 }
        let panelJSON = try edited.map { try promChatPanelJSON($0) } ?? ""
        let session: PromChatSession? = try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoNewPromChat(config, context, kubeServer, sourceJSON, namesJSON, panelJSON, $0).map(PromChatSession.init)
        }
        guard let session else { throw TalosError(message: "NewPromChat returned no chat") }
        return session
    }
}

/// A conversation Go holds in memory: the turns so far, the source and its metric names.
final class PromChatSession: @unchecked Sendable {
    private let go: IchorgoPromChat

    fileprivate init(_ go: IchorgoPromChat) {
        self.go = go
    }

    /// Sends `message` with the conversation so far and streams the answer; cancelling the
    /// consuming task stops waiting for it. An empty model means the provider's default, an
    /// empty baseURL its own API.
    func ask(provider: String, settings: AIProviderSettings, language: String, message: String) -> AsyncStream<PromChatEvent> {
        TalosClient.bridged { continuation in
            let bridge = PromChatBridge(
                answer: { continuation.yield(.answer($0)) },
                done: { panel, error in
                    continuation.yield(.done(panel: panel, error: error))
                    continuation.finish()
                }
            )
            let run = go.ask(provider, apiKey: settings.apiKey, model: settings.model, baseURL: settings.baseURL,
                             language: language, message: message, listener: bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }

    /// Forgets the conversation; the source and its metric names stay.
    func reset() {
        go.reset()
    }
}

private final class PromChatBridge: NSObject, IchorgoPromChatListenerProtocol, @unchecked Sendable {
    private let answer: @Sendable (String) -> Void
    private let done: @Sendable (PanelSuggestion?, String?) -> Void

    init(answer: @escaping @Sendable (String) -> Void, done: @escaping @Sendable (PanelSuggestion?, String?) -> Void) {
        self.answer = answer
        self.done = done
    }

    func onAnswer(_ text: String?) {
        answer(text ?? "")
    }

    func onDone(_ panelJSON: String?, errMessage: String?) {
        let panel = panelJSON.nonEmpty.flatMap { try? TalosJSON.decode(PanelSuggestion.self, from: $0) }
        done(panel, errMessage.nonEmpty)
    }
}
