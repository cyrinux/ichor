import SwiftUI
import IchorCore

/// The panel assistant of one Metrics screen: a conversation about the cluster's source,
/// started again when the source or the panel being edited changes. The metric names are
/// read once per source, best effort: without them the model uses the usual names.
@Observable
@MainActor
final class PanelChatModel {
    private(set) var messages: [PanelChatMessage] = []
    private(set) var asking = false
    /// The conversation is open: the source's metric names were looked up (or given up on).
    private(set) var ready = false
    private(set) var namesFailed = false
    /// Why the conversation could not be opened.
    private(set) var openError: String?

    private struct Key: Equatable {
        let source: PromSource
        let current: PromPanel?
    }

    private var session: PromChatSession?
    private var openedFor: Key?
    private var names: (source: PromSource, names: [String])?
    private var openTask: Task<Void, Never>?
    private var askTask: Task<Void, Never>?

    /// Opens the conversation for `source` and `current`; the same pair keeps the one on screen.
    func open(client: TalosClient, source: PromSource, current: PromPanel?) {
        let key = Key(source: source, current: current)
        if openedFor == key, session != nil || openTask != nil { return }
        openedFor = key
        stop()
        openTask?.cancel()
        session = nil
        messages = []
        ready = false
        namesFailed = false
        openError = nil
        openTask = Task {
            var known: [String] = []
            var failed = false
            if let cached = names, cached.source == source {
                known = cached.names
            } else {
                do {
                    known = try await client.promMetricNames(source)
                    names = (source, known)
                } catch {
                    failed = true
                }
            }
            guard !Task.isCancelled else { return }
            do {
                session = try await client.newPromChat(source, metricNames: known, current: current)
                ready = true
                namesFailed = failed
            } catch {
                openError = error.localizedDescription
            }
            openTask = nil
        }
    }

    func send(_ text: String, provider: String, settings: AIProviderSettings, language: String) {
        guard let session, !asking else { return }
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty else { return }
        messages.append(PanelChatMessage(fromUser: true, text: message))
        let answer = PanelChatMessage(fromUser: false, text: "")
        messages.append(answer)
        asking = true
        let events = session.ask(provider: provider, settings: settings, language: language, message: message)
        askTask = Task { @MainActor in
            for await event in events {
                // Cancelled (Stop, a new chat): what was still queued belongs to the old answer.
                guard !Task.isCancelled else { break }
                switch event {
                case .answer(let text): update(answer.id) { $0.text = text }
                case .done(let panel, let error): update(answer.id) { $0.panel = panel; $0.error = error }
                }
            }
            asking = false
            askTask = nil
        }
    }

    /// Stops waiting for the answer, keeping what was received; an empty answer goes.
    func stop() {
        askTask?.cancel()
        askTask = nil
        asking = false
        messages.removeAll { !$0.fromUser && $0.text.isEmpty && $0.panel == nil && $0.error == nil }
    }

    /// Forgets the conversation; the source and its metric names stay.
    func newChat() {
        stop()
        session?.reset()
        messages = []
    }

    private func update(_ id: UUID, _ change: (inout PanelChatMessage) -> Void) {
        if let i = messages.firstIndex(where: { $0.id == id }) { change(&messages[i]) }
    }
}

/// A chat with the model set up in Settings that proposes panels for the source, each
/// checked against it by the Go core; "Use this panel" hands one to the editor.
struct PanelChatSheet: View {
    let model: PanelChatModel
    let sourceLabel: String
    let onUse: (PanelSuggestion) -> Void

    @Environment(AISettings.self) private var ai
    @Environment(\.dismiss) private var dismiss
    @State private var input = ""

    private var providerName: String { ai.provider?.name ?? "" }

    var body: some View {
        NavigationStack {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 12) {
                        header
                        ForEach(model.messages) { message in
                            if message.fromUser {
                                UserBubble(text: message.text)
                            } else {
                                AnswerBubble(message: message, providerName: providerName, asking: model.asking, sourceLabel: sourceLabel, onUse: onUse)
                            }
                        }
                        if model.messages.contains(where: { !$0.fromUser && !$0.text.isEmpty }) {
                            Text("AI answers can be wrong. Check a command before running it, especially one that resets a node or changes etcd.")
                                .font(.footnote).foregroundStyle(.secondary)
                        }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                    .padding()
                }
                // The newest text stays in view while the answer is written.
                .onChange(of: model.messages) { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if ai.provider != nil, ai.current.canAsk { inputBar }
            }
            .navigationTitle("Panel assistant")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if !model.messages.isEmpty { Button("New chat") { model.newChat() } }
                }
            }
            .task { await ai.loadProviders() }
            .themedBackground()
        }
        .onDisappear { model.stop() }
    }

    @ViewBuilder private var header: some View {
        Text("Your messages, the source’s metric names and the current panel are sent to \(providerName) as they are.")
            .font(.footnote).foregroundStyle(.secondary)
        if let error = model.openError {
            Text(verbatim: error).foregroundStyle(.statusBad).font(.footnote)
        } else if !model.ready {
            HStack(spacing: 12) {
                ProgressView()
                Text("Reading the metric names…").font(.footnote)
            }
        } else if model.namesFailed {
            Text("The metric names could not be read: the model will use the usual ones.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        if ai.provider == nil || !ai.current.canAsk {
            Text("To read the answer here, add an API key in Settings.")
                .font(.footnote).foregroundStyle(.secondary)
        } else if model.messages.isEmpty {
            Text("Describe the panel you want: what to show, grouped by what, over which range.")
        }
    }

    private var inputBar: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField("Message", text: $input, axis: .vertical)
                .lineLimit(1...5)
                .textFieldStyle(.roundedBorder)
            if model.asking {
                Button("Stop") { model.stop() }
            } else {
                Button(action: send) { Image(systemName: "arrow.up.circle.fill").font(.title2) }
                    .accessibilityLabel(Text("Send"))
                    .disabled(!model.ready || input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
    }

    private func send() {
        guard let provider = ai.provider else { return }
        model.send(input, provider: provider.id, settings: ai.current, language: appLanguage)
        input = ""
    }
}

private struct UserBubble: View {
    let text: String

    var body: some View {
        HStack {
            Spacer(minLength: 40)
            Text(verbatim: text)
                .padding(10)
                .background(Color.accentColor.opacity(0.15), in: RoundedRectangle(cornerRadius: 12))
        }
    }
}

/// The model's answer: its explanation as it is written, then the panel it proposed.
private struct AnswerBubble: View {
    let message: PanelChatMessage
    let providerName: String
    let asking: Bool
    let sourceLabel: String
    let onUse: (PanelSuggestion) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if message.text.isEmpty, message.error == nil, asking {
                HStack(spacing: 12) {
                    ProgressView()
                    Text("Waiting for \(providerName)…").font(.footnote)
                }
            }
            if !message.text.isEmpty {
                Text(verbatim: message.text).textSelection(.enabled)
            }
            // A failure keeps what was already written above it.
            if let error = message.error { Text(verbatim: error).foregroundStyle(.statusBad) }
            if let panel = message.panel { PanelSuggestionCard(panel: panel, sourceLabel: sourceLabel, onUse: onUse) }
        }
    }
}

private struct PanelSuggestionCard: View {
    let panel: PanelSuggestion
    let sourceLabel: String
    let onUse: (PanelSuggestion) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(verbatim: panel.title.isEmpty ? panel.query : panel.title).font(.headline)
            Text(verbatim: panel.query).font(.footnote.monospaced()).textSelection(.enabled)
            Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
            status.font(.caption)
            if panel.attempts > 1, panel.verified {
                Text("Fixed after a failed check").font(.caption).foregroundStyle(.secondary)
            }
            HStack {
                Button("Use this panel") { onUse(panel) }.buttonStyle(.borderedProminent)
                Button { UIPasteboard.general.string = panel.query } label: { Label("Copy", systemImage: "doc.on.doc") }
            }
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12))
    }

    private var details: String {
        var line = String(localized: "Unit") + ": " + promUnitLabel(panel.unit)
        if !panel.legend.isEmpty { line += " · " + String(localized: "Legend") + ": " + panel.legend }
        return line
    }

    @ViewBuilder private var status: some View {
        if panel.verified, panel.empty {
            Text("Valid query, but no data in the last 15 minutes").foregroundStyle(.secondary)
        } else if panel.verified {
            Text("Checked against \(sourceLabel)").foregroundStyle(.statusOK)
        } else {
            Text("Not checked: \(panel.notice)").foregroundStyle(.secondary)
        }
    }
}
