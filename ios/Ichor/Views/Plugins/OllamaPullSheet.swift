import IchorCore
import SwiftUI

/// "Pull a model…" under an Ollama server row: the server's namespace and name are those of the
/// workload the row opens.
struct OllamaPullButton: View {
    let namespace: String
    let server: String
    /// After a pull that worked: the tab reads again, so the model shows.
    let onPulled: () -> Void

    @State private var pulling = false

    var body: some View {
        Button("Pull a model…") { pulling = true }
            .sheet(isPresented: $pulling) {
                OllamaPullSheet(namespace: namespace, server: server, onPulled: onPulled)
            }
    }
}

/// The model's name (name:tag, with a link to the library), then the pull: Ollama's step, a bar
/// with the bytes done of total, and Stop; then its outcome. The pull runs as long as the sheet
/// is open; closing it stops the download (the layers already there stay on the server).
struct OllamaPullSheet: View {
    let namespace: String
    let server: String
    let onPulled: () -> Void

    private enum Phase: Equatable {
        case asking
        case running(OllamaPullProgress?)
        case done
        case failed(String)
    }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var phase = Phase.asking
    @State private var task: Task<Void, Never>?

    private var modelName: String { name.trimmingCharacters(in: .whitespaces) }

    private var running: Bool {
        if case .running = phase { return true }
        return false
    }

    var body: some View {
        NavigationStack {
            Form {
                if phase == .asking {
                    askSection
                } else {
                    pullSection
                }
                Section {
                    Text(
                        "The download goes on while Ichor stays open, also in the background. If the system closes Ichor it stops; pulling again resumes it."
                    )
                    .note()
                }
            }
            .navigationTitle(Text("Pull a model onto \(server)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbar }
        }
        .interactiveDismissDisabled(running)
        .onDisappear { task?.cancel() }
    }

    private var askSection: some View {
        Section {
            TextField("Model", text: $name, prompt: Text("name:tag from the library"))
                .font(.body.monospaced())
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .keyboardType(.URL)
                .submitLabel(.go)
                .onSubmit(start)
            Link("Browse the Ollama library", destination: ollamaLibraryURL)
        }
    }

    @ViewBuilder
    private var pullSection: some View {
        Section {
            Text(verbatim: modelName).font(.headline.monospaced())
            switch phase {
            case .asking:
                EmptyView()
            case .running(let progress):
                OllamaPullProgressView(progress: progress)
            case .done:
                Label("Pulled: the model is on the server", systemImage: "checkmark.circle")
                    .foregroundStyle(Color.statusOK)
            case .failed(let message):
                Label {
                    Text(verbatim: message)
                } icon: {
                    Image(systemName: "exclamationmark.triangle")
                }
                .foregroundStyle(Color.statusWarn)
            }
        } footer: {
            Text(verbatim: "\(namespace)/\(server)")
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        switch phase {
        case .asking:
            ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            ToolbarItem(placement: .confirmationAction) {
                Button("Pull", action: start).disabled(!isOllamaModelName(modelName))
            }
        case .running:
            ToolbarItem(placement: .cancellationAction) {
                Button("Stop the download", role: .destructive) {
                    task?.cancel()
                    dismiss()
                }
            }
        case .done, .failed:
            ToolbarItem(placement: .confirmationAction) { Button("Close") { dismiss() } }
        }
    }

    private func start() {
        guard isOllamaModelName(modelName), let client = model.client, !running else { return }
        phase = .running(nil)
        let stream = client.ollamaPull(namespace: namespace, server: server, model: modelName)
        task = Task {
            do {
                for try await progress in stream {
                    phase = .running(progress)
                }
                guard !Task.isCancelled else { return }
                phase = .done
                onPulled()
            } catch {
                guard !Task.isCancelled else { return }
                phase = .failed(error.localizedDescription)
            }
        }
    }
}

/// The bar (indeterminate before the first layer) and Ollama's step with the bytes.
private struct OllamaPullProgressView: View {
    let progress: OllamaPullProgress?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let fraction = progress?.fraction {
                ProgressView(value: fraction)
            } else {
                ProgressView().frame(maxWidth: .infinity, alignment: .leading)
            }
            Text(verbatim: line)
                .font(.callout)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }

    private var line: String {
        guard let progress, !progress.status.isEmpty else { return String(localized: "Starting the pull…") }
        guard progress.total > 0 else { return progress.status }
        return String(localized: "\(progress.status) · \(progress.completedText) of \(progress.totalText)")
    }
}
