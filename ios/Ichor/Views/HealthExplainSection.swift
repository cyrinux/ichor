import SwiftUI
import Observation
import IchorCore

/// The health check helper's answer about a failed run. Owned by HealthView, so a re-run
/// clears it and leaving the screen stops it.
@Observable
@MainActor
final class HealthExplanation {
    /// Reading node readiness and events into the report.
    private(set) var collecting = false
    private(set) var text = ""
    /// Why it stopped early; `text` may still hold the part received.
    private(set) var error: String?
    private var task: Task<Void, Never>?
    /// Tells a finished run from one that was replaced or stopped.
    private var generation = 0

    var running: Bool { task != nil }

    /// Sends the failed run's lines, node readiness and recent events to the selected model.
    func explain(client: TalosClient, lines: [String], failure: String, ai: AISettings, provider: AIProvider) {
        task?.cancel()
        generation += 1
        let current = generation
        let settings = ai.current
        let anonymize = ai.anonymize
        text = ""
        error = nil
        collecting = true
        task = Task { @MainActor in
            defer {
                if current == generation {
                    collecting = false
                    task = nil
                }
            }
            do {
                let report = try await client.collectHealthExplanation(lines: lines, failure: failure, anonymize: anonymize)
                guard !Task.isCancelled else { return }
                collecting = false
                let events = report.ask(provider: provider.id, settings: settings, language: appLanguage,
                                        note: healthFailureNote(failure))
                for await event in events {
                    // Stopped or replaced: what was still queued belongs to the old answer.
                    guard !Task.isCancelled else { break }
                    switch event {
                    case .answer(let answer): text = answer
                    case .done(let failed): error = failed
                    }
                }
            } catch {
                if !Task.isCancelled { self.error = error.localizedDescription }
            }
        }
    }

    /// Stops collecting or waiting for the answer, keeping what was received.
    func stop() {
        task?.cancel()
        task = nil
        collecting = false
        generation += 1
    }

    /// Forgets the answer: it was about the previous run.
    func reset() {
        stop()
        text = ""
        error = nil
    }
}

/// Under a failed health check when AI is enabled: explains the failure in a few lines from
/// what the check said, and leads to the full diagnosis.
struct HealthExplainSection: View {
    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai
    let explanation: HealthExplanation
    let lines: [String]
    let failure: String

    var body: some View {
        Section {
            if explanation.running {
                HStack {
                    ProgressView()
                    Text("Waiting for \(ai.provider?.name ?? "")…")
                    Spacer()
                    Button("Stop") { explanation.stop() }
                }
            } else if let provider = ai.provider, ai.current.canAsk {
                Button("Explain with AI") {
                    guard let client = model.client else { return }
                    explanation.explain(client: client, lines: lines, failure: failure, ai: ai, provider: provider)
                }
            } else {
                Text("To read the answer here, add an API key in Settings.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if !explanation.text.isEmpty {
                Text(verbatim: explanation.text).textSelection(.enabled)
            }
            // A failure keeps what was already written above it.
            if let error = explanation.error {
                Text(verbatim: error).foregroundStyle(.statusBad)
            }
            if !explanation.running, !explanation.text.isEmpty {
                Button { UIPasteboard.general.string = explanation.text } label: { Label("Copy", systemImage: "doc.on.doc") }
            }
            NavigationLink("Continue in Diagnosis", value: Route.diagnosis(note: healthFailureNote(failure)))
        } header: {
            Text("What went wrong")
        } footer: {
            if !explanation.text.isEmpty {
                Text("AI answers can be wrong. Check a command before running it, especially one that resets a node or changes etcd.")
            } else if let provider = ai.provider, ai.current.canAsk {
                Text("Sends the failed checks, node readiness and recent events to \(provider.name).")
            }
        }
    }
}
