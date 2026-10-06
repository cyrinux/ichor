import Foundation
import Ichorgo
import IchorCore

enum DiagnosisEvent: Sendable {
    /// The whole answer so far, not only what is new.
    case answer(String)
    case done(error: String?)
}

/// A report about the cluster collected for the AI diagnosis. Go holds it in memory only,
/// with what it needs to put the real names back in an answer written about placeholders.
final class DiagnosisReport: @unchecked Sendable {
    private let go: IchorgoDiagnosis
    /// The text that would be sent, to show to the user first.
    let text: String
    /// Names and addresses in `text` are placeholders.
    let anonymized: Bool

    fileprivate init(_ go: IchorgoDiagnosis) {
        self.go = go
        text = go.report()
        anonymized = go.anonymized()
    }

    /// The whole question as one text (instructions, report, note), to hand to another app.
    /// `language` is the app's language code ("fr").
    func prompt(language: String, note: String) -> String {
        go.prompt(language, note: note)
    }

    /// Sends the report and the note to the model and streams its answer; cancelling the
    /// consuming task stops waiting for it. An empty model means the provider's default, an
    /// empty baseURL its own API.
    func ask(provider: String, settings: AIProviderSettings, language: String, note: String) -> AsyncStream<DiagnosisEvent> {
        Self.bridged { continuation in
            let bridge = DiagnosisBridge(
                answer: { continuation.yield(.answer($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = go.ask(provider, apiKey: settings.apiKey, model: settings.model, baseURL: settings.baseURL,
                             language: language, note: note, listener: bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

/// AI diagnosis calls (see TalosClient for the conventions).
extension TalosClient {
    /// The providers the diagnosis can use, with their default model (no network).
    static func aiProviders() async throws -> [AIProvider] {
        try await json { IchorgoAIProviders($0) }
    }

    /// The models the key can use, newest first; also tells whether the key and URL work.
    static func aiModels(provider: String, settings: AIProviderSettings) async throws -> [AIModel] {
        try await json { IchorgoAIModels(provider, settings.apiKey, settings.baseURL, $0) }
    }

    /// Reads the cluster state into a report (os:reader calls, plus Argo CD and Flux through the
    /// Kubernetes API with os:admin). Nothing leaves the phone here.
    func collectDiagnosis(anonymize: Bool) async throws -> DiagnosisReport {
        let report: DiagnosisReport? = try await Self.run { [config, context, kubeServer] error in
            IchorgoCollectDiagnosis(config, context, kubeServer, anonymize, error).map(DiagnosisReport.init)
        }
        guard let report else { throw TalosError(message: "CollectDiagnosis returned no report") }
        return report
    }
}

private final class DiagnosisBridge: NSObject, IchorgoDiagnosisListenerProtocol, @unchecked Sendable {
    private let answer: @Sendable (String) -> Void
    private let done: @Sendable (String?) -> Void

    init(answer: @escaping @Sendable (String) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.answer = answer
        self.done = done
    }

    func onAnswer(_ text: String?) {
        answer(text ?? "")
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
