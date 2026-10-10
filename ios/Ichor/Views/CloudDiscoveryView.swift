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

    /// Opens on `provider`, one of kubeDiscoverProviders.
    init(provider: String = kubeDiscoverProviders[0], onFound: @escaping (DiscoveredClusters) -> Void) {
        self.onFound = onFound
        _provider = State(initialValue: provider)
    }

    @Environment(\.dismiss) private var dismiss
    @State private var provider: String
    @State private var fieldsByProvider: [String: [String]] = [:]
    @State private var optionsByProvider: [String: [[String]]] = [:]
    @State private var option = 0
    @State private var values: [String: String] = [:]
    @State private var busy = false
    @State private var progress: DiscoveryProgress?
    @State private var error: String?

    /// The credentials the provider takes (GKE: a service account key or gcloud user credentials).
    private var sets: [[String]] {
        kubeDiscoverOptionSets(provider: provider, fields: fieldsByProvider, options: optionsByProvider)
    }

    private var fields: [String] { sets.indices.contains(option) ? sets[option] : sets.first ?? [] }

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
                if sets.count > 1 {
                    Section {
                        Picker("Sign in with", selection: $option) {
                            ForEach(sets.indices, id: \.self) { index in
                                Text(KubeAuthWording.optionLabel(sets[index])).tag(index)
                            }
                        }
                        .pickerStyle(.segmented)
                    }
                }
                if !fields.isEmpty {
                    KubeFieldsSection(fields: fields, values: $values)
                }
                let hints = fields.compactMap(KubeAuthWording.fieldHint)
                if !hints.isEmpty {
                    Section {
                        ForEach(hints, id: \.self) { Text($0).font(.footnote).foregroundStyle(.secondary) }
                    }
                }
                Section {
                    if busy { DiscoverProgressRow(progress: progress) }
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
        .onChange(of: provider) { option = 0; values = [:]; error = nil }
        .onChange(of: option) { values = [:]; error = nil }
    }

    private func loadFields() async {
        do {
            fieldsByProvider = try await TalosClient.discoverFields()
            optionsByProvider = try await TalosClient.discoverOptions()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func discover() async {
        busy = true
        progress = nil
        let poll = Task { await pollProgress() }
        defer {
            poll.cancel()
            busy = false
        }
        let secrets = kubeSecretsJSON(fields: fields, values: values)
        do {
            let kubeconfig = try await TalosClient.discoverClusters(provider: provider, secrets: secrets)
            onFound(DiscoveredClusters(kubeconfig: kubeconfig, secrets: secrets))
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Shows how far the running discovery got until cancelled.
    private func pollProgress() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .milliseconds(250))
            if let read = TalosClient.discoverProgress(), read.running, busy { progress = read }
        }
    }
}

/// How far the running discovery got: a bar that fills as a Google account's projects are read
/// (indeterminate while they are listed, and for the other clouds), and the counts.
private struct DiscoverProgressRow: View {
    let progress: DiscoveryProgress?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let progress, let fraction = progress.fraction {
                ProgressView(value: fraction)
                Text("Scanned \(String(progress.scanned)) of \(String(progress.projects)) projects · \(String(progress.clusters)) clusters found")
                    .font(.footnote).foregroundStyle(.secondary)
                    .monospacedDigit()
            } else {
                ProgressView().frame(maxWidth: .infinity, alignment: .leading)
                Text("Looking for clusters…").font(.footnote).foregroundStyle(.secondary)
            }
        }
        .animation(.default, value: progress)
    }
}
