import SwiftUI
import IchorCore

/// Settings of the optional AI diagnosis. Off by default; while off, nothing else of the
/// feature shows in the app.
struct AISection: View {
    @Environment(AISettings.self) private var ai
    @State private var models: [AIModel] = []
    @State private var listing = false
    @State private var modelsMessage: String?

    var body: some View {
        @Bindable var ai = ai
        Section {
            Toggle("AI diagnosis", isOn: $ai.enabled)
            if ai.enabled, let provider = ai.provider {
                Picker("Provider", selection: Binding(get: { provider.id }, set: { ai.select($0) })) {
                    ForEach(ai.providers) { item in
                        if item.id == AISettings.deviceProvider {
                            Text("This phone").tag(item.id)
                        } else {
                            Text(verbatim: item.name).tag(item.id)
                        }
                    }
                }
                if ai.onPhone {
                    LocalModelSection()
                } else {
                    keyRows(provider)
                }
                VStack(alignment: .leading, spacing: 4) {
                    Toggle("Hide names and addresses", isOn: $ai.anonymize)
                    Text(
                        "The nodes’ names, IP addresses and domain are replaced with placeholders before the report is sent. The rest of the logs, such as pod names and other host names, is sent as it is."
                    )
                    .font(.caption).foregroundStyle(.secondary)
                }
                if !ai.onPhone {
                    VStack(alignment: .leading, spacing: 4) {
                        Toggle("Let the model ask for more", isOn: $ai.allowTools)
                        Text(
                            "The model may read a pod’s logs, an object’s events or YAML, or a Talos service’s logs, three times per question. Each request shows with the answer, and what it reads is masked like the report."
                        )
                        .font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        } header: {
            Text("AI diagnosis")
        } footer: {
            Text(
                "Optional. Ask an AI model what is wrong with the cluster and how to fix it. Nothing is sent until you ask."
            )
        }
        .task { await ai.loadProviders() }
        // Another provider, key or server has another list.
        .onChange(of: ai.provider?.id) { resetModels() }
        .onChange(of: ai.current.apiKey) { resetModels() }
        .onChange(of: ai.current.baseURL) { resetModels() }
    }

    /// The API key, the model and the server of a provider reached over the network.
    @ViewBuilder
    private func keyRows(_ provider: AIProvider) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            SecureField("API key", text: Binding(get: { ai.current.apiKey }, set: { ai.setAPIKey($0) }))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Text("Stored encrypted on this device. Only needed to read the answer inside the app.")
                .font(.caption).foregroundStyle(.secondary)
        }
        if let url = URL(string: provider.keyUrl) {
            Link("Get an API key", destination: url)
        }
        modelRows(provider)
        VStack(alignment: .leading, spacing: 4) {
            TextField("Server URL (optional)", text: Binding(get: { ai.current.baseURL }, set: { ai.setBaseURL($0) }))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
            Text("Only for a gateway or a local model. Leave empty to use the provider’s own API.")
                .font(.caption).foregroundStyle(.secondary)
        }
    }

    /// The model id, typed or picked from the provider's list.
    @ViewBuilder
    private func modelRows(_ provider: AIProvider) -> some View {
        TextField(
            "Model", text: Binding(get: { ai.current.model }, set: { ai.setModel($0) }),
            prompt: Text("Default: \(provider.defaultModel)")
        )
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
        if models.isEmpty {
            Button("List models") { Task { await listModels(provider) } }
                .disabled(listing || !ai.current.canAsk)
        } else {
            Menu("List models") {
                ForEach(models) { model in
                    Button {
                        ai.setModel(model.id)
                    } label: {
                        Text(verbatim: model.label)
                    }
                }
            }
        }
        if let modelsMessage { Text(modelsMessage).font(.footnote).foregroundStyle(.statusBad) }
    }

    private func listModels(_ provider: AIProvider) async {
        listing = true
        defer { listing = false }
        modelsMessage = nil
        do {
            let listed = try await TalosClient.aiModels(provider: provider.id, settings: ai.current)
            // The settings changed while the list was loading: it is another provider's.
            guard provider.id == ai.provider?.id else { return }
            models = listed
            if listed.isEmpty { modelsMessage = String(localized: "No model found") }
        } catch {
            modelsMessage = error.localizedDescription
        }
    }

    private func resetModels() {
        models = []
        modelsMessage = nil
    }
}
