import SwiftUI
import IchorCore

/// AI diagnosis: shows the report about the cluster, then sends it (only when asked) to the
/// model set up in the settings, or hands it to another app through the share sheet.
struct DiagnosisView: View {
    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai
    @State private var state: LoadState<DiagnosisReport> = .loading
    @State private var note: String
    @State private var showReport = false
    @State private var answer = ""
    @State private var answerError: String?
    @State private var asking: Task<Void, Never>?

    /// `initialNote`: what the screen that opened this one already knows (a failed health check).
    init(initialNote: String = "") {
        _note = State(initialValue: initialNote)
    }

    var body: some View {
        Group {
            switch state {
            case .loading:
                ProgressView("Reading the cluster state…").frame(maxWidth: .infinity, maxHeight: .infinity)
            case .failed(let message):
                ContentUnavailableView {
                    Label("Request failed", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(message)
                } actions: {
                    Button("Retry") { Task { await collect() } }
                }
            case .loaded(let report, let at, _):
                content(report)
                    .safeAreaInset(edge: .bottom, spacing: 0) { FreshnessFooter(at: at) }
            }
        }
        .navigationTitle("AI diagnosis")
        // A new report when the context, the screenshot mode or the anonymization changes.
        .task(id: "\(model.activeContext)#\(model.dataGeneration)#\(ai.anonymize)") {
            await ai.loadProviders()
            await collect()
        }
        .onDisappear { asking?.cancel() }
    }

    private func content(_ report: DiagnosisReport) -> some View {
        List {
            Section {
                Text(report.anonymized
                     ? String(localized: "This report is what gets sent, and only when you ask. The nodes’ names, IP addresses and domain are replaced with placeholders; the rest of the logs, such as pod names and other host names, is sent as it is.")
                     : String(localized: "This report is what gets sent, and only when you ask. It contains the real node names and IP addresses."))
                    .font(.footnote).foregroundStyle(.secondary)
                HStack {
                    Text("Report (\(reportSize(report.text)))")
                    Spacer()
                    Button(showReport ? String(localized: "Hide") : String(localized: "Show")) { showReport.toggle() }
                }
                if showReport {
                    Text(verbatim: report.text)
                        .font(.system(.caption2, design: .monospaced))
                        .textSelection(.enabled)
                }
            }
            Section {
                TextField("What is going wrong? (optional)", text: $note, axis: .vertical)
                    .lineLimit(1...4)
                if let provider = ai.provider, ai.current.canAsk {
                    if asking != nil {
                        HStack {
                            ProgressView()
                            Text("Waiting for \(provider.name)…")
                            Spacer()
                            Button("Stop") { asking?.cancel() }
                        }
                    } else {
                        Button("Ask \(provider.name)") { ask(report, provider: provider) }
                    }
                } else {
                    Text("To read the answer here, add an API key in Settings.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                SharePromptLink(report: report, note: note)
            } footer: {
                Text("Sends the question and the report to an app you choose, such as Claude, ChatGPT or Gemini. No API key needed.")
            }
            if !answer.isEmpty || answerError != nil {
                Section {
                    if !answer.isEmpty {
                        Text(verbatim: answer).textSelection(.enabled)
                    }
                    // A failure keeps what was already written above it.
                    if let answerError { Text(verbatim: answerError).foregroundStyle(.red) }
                    if asking == nil, !answer.isEmpty {
                        Button { UIPasteboard.general.string = answer } label: { Label("Copy", systemImage: "doc.on.doc") }
                    }
                } header: {
                    Text("Answer")
                } footer: {
                    Text("AI answers can be wrong. Check a command before running it, especially one that resets a node or changes etcd.")
                }
            }
        }
        .refreshable { await collect() }
        .themedBackground()
    }

    private func collect() async {
        guard let client = model.client else { return }
        asking?.cancel()
        answer = ""
        answerError = nil
        state = .loading
        state = await .from { try await client.collectDiagnosis(anonymize: ai.anonymize) }
    }

    private func ask(_ report: DiagnosisReport, provider: AIProvider) {
        let events = report.ask(provider: provider.id, settings: ai.current, language: appLanguage, note: note)
        answer = ""
        answerError = nil
        asking = Task { @MainActor in
            for await event in events {
                // Cancelled (Stop, a new report): what was still queued belongs to the old answer.
                guard !Task.isCancelled else { break }
                switch event {
                case .answer(let text): answer = text
                case .done(let error): answerError = error
                }
            }
            asking = nil
        }
    }
}

/// The language the app is shown in ("fr"): the model answers in it.
private var appLanguage: String { Bundle.main.preferredLocalizations.first ?? "en" }

/// Hands the whole question to the app the user picks in the share sheet (Claude, ChatGPT,
/// Gemini…). A view of its own, so the prompt is only rebuilt when the note changes, not
/// with every piece of a streamed answer.
private struct SharePromptLink: View {
    let report: DiagnosisReport
    let note: String

    var body: some View {
        ShareLink(item: report.prompt(language: appLanguage, note: note)) {
            Label("Share with an assistant app", systemImage: "square.and.arrow.up")
        }
    }
}
