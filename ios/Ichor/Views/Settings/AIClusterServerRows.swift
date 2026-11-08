import IchorCore
import SwiftUI

/// The cluster's Ollama as the AI server: which server, whether its port-forward runs (and to
/// which pod), and a reconnect when it does not. The server is picked on its row in Data
/// services, Ollama.
struct AIClusterServerRows: View {
    @Environment(AISettings.self) private var ai
    @State private var forward = AIForward.shared
    @State private var connecting = false
    @State private var message: String?

    var body: some View {
        if let ref = OllamaServerRef(stored: ai.current.baseURL) {
            Text("Server: \(ref.text)")
            if let active = forward.forward, active.isFor(ref) {
                Label("Via port-forward to \(active.target)", systemImage: "arrow.left.arrow.right")
                    .foregroundStyle(.statusOK)
            } else {
                Button("The forward to the server is not running: tap to reconnect") {
                    Task { await connect(ref) }
                }
                .disabled(connecting)
            }
            if let message { Text(message).font(.footnote).foregroundStyle(.statusBad) }
        }
        Text(
            "Pick a server in Data services, Ollama: Use for AI diagnosis on its row. The report is masked as before and does not leave the cluster's network."
        )
        .font(.caption).foregroundStyle(.secondary)
        // It may have ended while the app was in the background.
        .task { await forward.refresh() }
    }

    private func connect(_ ref: OllamaServerRef) async {
        connecting = true
        defer { connecting = false }
        message = nil
        do {
            _ = try await forward.connect(ref)
        } catch {
            message = error.localizedDescription
        }
    }
}

/// "Use for AI diagnosis" under an Ollama server row: opens a port-forward to a ready pod of
/// the server and makes it the AI provider (Settings, AI diagnosis shows it). The demo cluster
/// refuses it with the Go core's message.
struct OllamaAIButton: View {
    let namespace: String
    let server: String

    @Environment(AISettings.self) private var ai
    @State private var busy = false
    @State private var message: String?
    @State private var succeeded = false

    var body: some View {
        let ref = OllamaServerRef(namespace: namespace, server: server)
        let inUse = ai.enabled && ai.inCluster && ai.current.baseURL == ref.text
        VStack(alignment: .leading, spacing: 4) {
            Button("Use for AI diagnosis") { Task { await use(ref) } }
                .disabled(busy || inUse)
            if let message {
                Text(message).font(.footnote).foregroundStyle(succeeded ? Color.statusOK : Color.statusBad)
            }
        }
    }

    private func use(_ ref: OllamaServerRef) async {
        busy = true
        defer { busy = false }
        message = nil
        do {
            await ai.loadProviders()
            _ = try await AIForward.shared.connect(ref)
            ai.useClusterServer(ref)
            succeeded = true
            message = String(localized: "The AI diagnosis now asks \(ref.text)")
        } catch {
            succeeded = false
            message = error.localizedDescription
        }
    }
}
