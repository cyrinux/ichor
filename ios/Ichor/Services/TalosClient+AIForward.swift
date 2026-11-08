import Foundation
import Ichorgo
import IchorCore
import Observation

/// The cluster's Ollama as the AI server: the Go core keeps one port-forward to a ready pod of
/// the chosen server (KubeOllamaForward). Leaving the cluster stops it (CancelCalls, in Go).
extension TalosClient {
    /// Opens (or keeps) the forward to the Ollama server `namespace`/`server` of this cluster.
    func ollamaForward(namespace: String, server: String) async throws -> OllamaForward {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeOllamaForward(config, context, kubeServer, namespace, server, $0)
        }
    }

    /// The forward running now, nil when none (never opened, stopped, or ended).
    static func ollamaForwardState() async -> OllamaForward? {
        let json: String? = try? await run { IchorgoOllamaForwardState($0) }
        guard let json, !json.isEmpty else { return nil }
        return try? JSONDecoder().decode(OllamaForward.self, from: Data(json.utf8))
    }

    static func stopOllamaForward() {
        IchorgoStopOllamaForward()
    }
}

/// The forward as the settings show it, and the base URL the AI calls get. One for the app:
/// the AI settings point at one server.
@Observable
@MainActor
final class AIForward {
    static let shared = AIForward()

    /// Whose client opens the forward (the cluster on screen); IchorApp sets it.
    @ObservationIgnored weak var model: AppModel?

    /// The forward running now, as last seen.
    private(set) var forward: OllamaForward?

    /// Opens (or keeps) the forward to `ref` on the open cluster.
    func connect(_ ref: OllamaServerRef) async throws -> OllamaForward {
        guard let client = model?.client else { throw TalosError(message: "no cluster is open") }
        let opened = try await client.ollamaForward(namespace: ref.namespace, server: ref.server)
        forward = opened
        return opened
    }

    /// `settings` with the forward's base URL for the cluster's Ollama, opened first if needed.
    func resolved(_ settings: AIProviderSettings) async throws -> AIProviderSettings {
        guard let ref = OllamaServerRef(stored: settings.baseURL) else {
            return AIProviderSettings(apiKey: "", model: settings.model, baseURL: "")
        }
        let opened = try await connect(ref)
        return AIProviderSettings(apiKey: "", model: settings.model, baseURL: opened.baseUrl)
    }

    /// Reads whether the forward still runs (it may have ended in the background).
    func refresh() async {
        forward = await TalosClient.ollamaForwardState()
    }

    /// Another provider was picked.
    func stop() {
        forward = nil
        Task { await offMainActor { TalosClient.stopOllamaForward() } }
    }
}

/// An answer stream for the cluster's Ollama: the forward opens first (a failure is the
/// stream's error, as `failed` words it), then `ask` streams. Ending the stream ends both.
func askThroughForward<Event: Sendable>(
    _ settings: AIProviderSettings,
    failed: @escaping @Sendable (String) -> Event,
    ask: @escaping @Sendable (AIProviderSettings) -> AsyncStream<Event>
) -> AsyncStream<Event> {
    AsyncStream { continuation in
        let task = Task {
            do {
                let resolved = try await AIForward.shared.resolved(settings)
                for await event in ask(resolved) {
                    continuation.yield(event)
                }
            } catch {
                continuation.yield(failed(error.localizedDescription))
            }
            continuation.finish()
        }
        continuation.onTermination = { _ in task.cancel() }
    }
}
