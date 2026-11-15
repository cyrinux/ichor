import SwiftUI
import Observation
import IchorCore

/// One subject's "Explain with AI": its report, read as soon as the section shows (nothing is
/// sent), then the answer streamed after a tap. Owned by the section's view.
@Observable
@MainActor
final class SubjectExplanation {
    private(set) var report: LoadState<DiagnosisReport> = .loading
    private(set) var asking = false
    private(set) var text = ""
    /// Why it stopped early; `text` may still hold the part received.
    private(set) var error: String?
    private var task: Task<Void, Never>?

    /// Nothing is sent before the report is read and shown.
    var reportReady: Bool {
        if case .loaded = report { return true }
        return false
    }

    /// Reads the report; a new subject, setting or screenshot mode reads it again.
    func load(client: TalosClient, subject: ExplainSubject, anonymize: Bool) async {
        stop()
        text = ""
        error = nil
        report = .loading
        do {
            let json = try subject.json()
            let read = try await client.collectExplanation(kind: subject.kind, subjectJSON: json, anonymize: anonymize)
            guard !Task.isCancelled else { return }
            report = .loaded(read, at: Date())
        } catch {
            if !Task.isCancelled { report = .failed(error.localizedDescription) }
        }
    }

    /// Sends the report shown, with `note`, to the selected model.
    func explain(note: String, ai: AISettings, provider: AIProvider) {
        guard case .loaded(let read, _, _) = report else { return }
        task?.cancel()
        let settings = ai.current
        text = ""
        error = nil
        asking = true
        task = Task { @MainActor in
            defer { asking = false }
            let events = read.ask(
                provider: provider.id, settings: settings, language: appLanguage, note: note, allowTools: false)
            for await event in events {
                // Stopped: what was still queued belongs to the old answer.
                guard !Task.isCancelled else { break }
                switch event {
                case .answer(let answer): text = answer
                case .tool, .toolDone: break
                case .done(let failed): error = failed
                }
            }
        }
    }

    /// Stops waiting for the answer, keeping what was received.
    func stop() {
        task?.cancel()
        task = nil
        asking = false
    }
}

/// "Explain with AI" for a pod, an alert or a checkup finding, in its sheet when AI is enabled:
/// the report it would send, the answer, and the full diagnosis with `note` already written.
struct ExplainSection: View {
    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai
    let subject: ExplainSubject
    let note: String
    @State private var explanation = SubjectExplanation()
    @State private var showReport = true

    var body: some View {
        Section {
            if explanation.asking {
                HStack {
                    ProgressView()
                    Text("Waiting for \(ai.providerName)…")
                    Spacer()
                    Button("Stop") { explanation.stop() }
                }
            } else if let provider = ai.provider, ai.canAsk {
                Button("Explain with AI") { explanation.explain(note: note, ai: ai, provider: provider) }
                    .disabled(!explanation.reportReady)
            } else {
                NotReadyHint(onPhone: ai.onPhone)
            }
            ExplainAnswerRows(text: explanation.text, error: explanation.error, running: explanation.asking)
            reportRows
            NavigationLink {
                DiagnosisView(initialNote: note)
            } label: {
                Text("Continue in Diagnosis")
            }
        } header: {
            Text("What is going on")
        } footer: {
            footer
        }
        .task(id: "\(model.dataGeneration)#\(ai.anonymize)") {
            guard let client = model.client else { return }
            await explanation.load(client: client, subject: subject, anonymize: ai.anonymize)
        }
        .onDisappear { explanation.stop() }
    }

    @ViewBuilder private var reportRows: some View {
        switch explanation.report {
        case .loading:
            HStack {
                ProgressView()
                Text("Reading the cluster state…").foregroundStyle(.secondary)
            }
        case .failed(let message):
            Text(verbatim: message).foregroundStyle(.statusBad)
        case .loaded(let report, _, _):
            HStack {
                Text("Report (\(reportSize(report.text)))")
                Spacer()
                Button(showReport ? String(localized: "Hide") : String(localized: "Show")) { showReport.toggle() }
                    .buttonStyle(.borderless)
            }
            if showReport {
                Text(verbatim: report.text)
                    .font(.system(.caption2, design: .monospaced))
                    .textSelection(.enabled)
            }
        }
    }

    @ViewBuilder private var footer: some View {
        if !explanation.text.isEmpty {
            Text(
                "AI answers can be wrong. Check a command before running it, especially one that resets a node or changes etcd."
            )
        } else if ai.onPhone, ai.canAsk {
            Text("Reads the report below with the model on this phone. Nothing leaves it.")
        } else if let provider = ai.provider, ai.canAsk {
            switch subject {
            case .pod:
                Text("Sends this pod’s state, its events and its last log lines, shown below, to \(provider.name).")
            case .alert:
                Text(
                    "Sends this alert’s labels and annotations, and the events of what it names, shown below, to \(provider.name)."
                )
            case .finding: Text("Sends this finding and the events of what it names, shown below, to \(provider.name).")
            }
        }
    }
}

/// The answer an Explain with AI section streams, the error it stopped on, and Copy once done.
struct ExplainAnswerRows: View {
    let text: String
    let error: String?
    let running: Bool

    var body: some View {
        if !text.isEmpty {
            Text(verbatim: text).textSelection(.enabled)
        }
        // A failure keeps what was already written above it.
        if let error {
            Text(verbatim: error).foregroundStyle(.statusBad)
        }
        if !running, !text.isEmpty {
            Button {
                UIPasteboard.general.string = text
            } label: {
                Label("Copy", systemImage: "doc.on.doc")
            }
        }
    }
}

/// An Explain with AI section on a page of its own, pushed from a sheet that has no list.
struct ExplainPage: View {
    let subject: ExplainSubject
    let note: String

    var body: some View {
        List { ExplainSection(subject: subject, note: note) }
            .themedBackground()
            .navigationTitle("Explain with AI")
            .navigationBarTitleDisplayMode(.inline)
    }
}
