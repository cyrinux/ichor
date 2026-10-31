import Foundation
import Ichorgo
import IchorCore

enum DiagnosisEvent: Sendable {
    /// The whole answer so far, not only what is new.
    case answer(String)
    /// The model asks for more, in the placeholders it used.
    case tool(name: String, argsJSON: String)
    /// What went back: `result` is the exact text sent. An empty name: the server takes no tools.
    case toolDone(name: String, bytes: Int, result: String, error: String?)
    case done(error: String?)
}

/// A report about the cluster collected for the AI diagnosis. Go holds it in memory only,
/// with what it needs to put the real names back in an answer written about placeholders.
final class DiagnosisReport: Sendable {
    private let go: GoObject<IchorgoDiagnosis>
    /// The text that would be sent, to show to the user first.
    let text: String
    /// Names and addresses in `text` are placeholders.
    let anonymized: Bool

    fileprivate init(_ go: IchorgoDiagnosis) {
        self.go = GoObject(go)
        text = go.report()
        anonymized = go.anonymized()
    }

    /// The whole question as one text (instructions, report, note), to hand to another app.
    /// `language` is the app's language code ("fr").
    func prompt(language: String, note: String) -> String {
        go.value.prompt(language, note: note)
    }

    /// Sends the report and the note to the model and streams its answer; cancelling the
    /// consuming task stops waiting for it. An empty model means the provider's default, an
    /// empty baseURL its own API.
    func ask(
        provider: String, settings: AIProviderSettings, language: String, note: String, allowTools: Bool
    ) -> AsyncStream<DiagnosisEvent> {
        TalosClient.bridged { continuation in
            let bridge = DiagnosisBridge(
                answer: { continuation.yield(.answer($0)) },
                event: { continuation.yield($0) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = go.value.ask(
                provider, apiKey: settings.apiKey, model: settings.model, baseURL: settings.baseURL,
                language: language, note: note, allowTools: allowTools, listener: bridge)
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

    /// Builds the short report the health check helper sends: the check's lines as shown, its
    /// failure, node readiness and recent warning/error events. Nothing leaves the phone here.
    func collectHealthExplanation(lines: [String], failure: String, anonymize: Bool) async throws -> DiagnosisReport {
        let linesJSON = try TalosJSON.encode(lines)
        let report: DiagnosisReport? = try await Self.run { [config, context, kubeServer] error in
            IchorgoCollectHealthExplanation(config, context, kubeServer, linesJSON, failure, anonymize, error).map(
                DiagnosisReport.init)
        }
        guard let report else { throw TalosError(message: "CollectHealthExplanation returned no report") }
        return report
    }
}

private final class DiagnosisBridge: NSObject, IchorgoDiagnosisListenerProtocol, Sendable {
    private let answer: @Sendable (String) -> Void
    private let event: @Sendable (DiagnosisEvent) -> Void
    private let done: @Sendable (String?) -> Void

    init(
        answer: @escaping @Sendable (String) -> Void,
        event: @escaping @Sendable (DiagnosisEvent) -> Void,
        done: @escaping @Sendable (String?) -> Void
    ) {
        self.answer = answer
        self.event = event
        self.done = done
    }

    func onAnswer(_ text: String?) {
        answer(text ?? "")
    }

    func onTool(_ name: String?, argsJSON: String?) {
        event(.tool(name: name ?? "", argsJSON: argsJSON ?? "{}"))
    }

    func onToolDone(_ name: String?, bytes: Int, result: String?, errMessage: String?) {
        event(.toolDone(name: name ?? "", bytes: bytes, result: result ?? "", error: errMessage.nonEmpty))
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
