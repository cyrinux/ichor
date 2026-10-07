import SwiftUI
import IchorCore

/// What cloud discovery found: a kubeconfig of the account's clusters for the import preview,
/// and the credentials that found them, to sign the added clusters in with.
struct DiscoveredClusters: Equatable {
    let kubeconfig: String
    /// Field → value JSON (see kubeSecretsJSON).
    let secrets: String
}

/// "Add from a cloud account" (K7): a provider, its account credentials, and the clusters
/// they reach (Go DiscoverClusters), previewed like an imported kubeconfig.
struct CloudDiscoveryView: View {
    let onFound: (DiscoveredClusters) -> Void

    init(onFound: @escaping (DiscoveredClusters) -> Void) {
        self.onFound = onFound
    }

    @Environment(\.dismiss) private var dismiss
    @State private var provider = kubeDiscoverProviders[0]
    @State private var fieldsByProvider: [String: [String]] = [:]
    @State private var values: [String: String] = [:]
    @State private var busy = false
    @State private var error: String?

    private var fields: [String] { fieldsByProvider[provider] ?? [] }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Provider", selection: $provider) {
                        ForEach(kubeDiscoverProviders, id: \.self) { provider in
                            Text(verbatim: KubeAuthWording.providerLabel(provider)).tag(provider)
                        }
                    }
                } footer: {
                    Text("Ichor lists the account's clusters with these credentials, then signs the clusters you add in with them. They stay on this device, sealed, and in your encrypted backups. Prefer a dedicated identity with read-only access.")
                }
                if !fields.isEmpty {
                    KubeFieldsSection(fields: fields, values: $values)
                }
                Section {
                    if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
                    Button {
                        Task { await discover() }
                    } label: {
                        HStack {
                            Text("Find clusters")
                            if busy { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(busy || fields.isEmpty || !kubeFieldsComplete(fields, values: values))
                }
            }
            .navigationTitle("Add from a cloud account")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
            }
        }
        .interactiveDismissDisabled(busy)
        .task { await loadFields() }
        // Another provider: its own fields, nothing typed for the previous one carried over.
        .onChange(of: provider) { values = [:]; error = nil }
    }

    private func loadFields() async {
        do {
            fieldsByProvider = try await TalosClient.discoverFields()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func discover() async {
        busy = true
        defer { busy = false }
        let secrets = kubeSecretsJSON(fields: fields, values: values)
        do {
            let kubeconfig = try await TalosClient.discoverClusters(provider: provider, secrets: secrets)
            onFound(DiscoveredClusters(kubeconfig: kubeconfig, secrets: secrets))
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }
}
