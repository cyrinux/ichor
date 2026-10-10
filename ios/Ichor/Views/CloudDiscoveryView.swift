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
    @State private var flow = KubeSignInFlow()
    @State private var work: Task<Void, Never>?
    /// Credentials stay in memory; Go holds the corresponding tokens until import finishes.
    @State private var pendingSecrets: String?
    @State private var signedSecrets: String?
    @State private var handedOff = false

    private var working: Bool { busy || flow.isRunning }
    private var google: Bool { fields.contains(kubeGoogleSignInField) }
    private var visibleFields: [String] { fields.filter { $0 != kubeGoogleSignInField } }
    private var complete: Bool { !fields.isEmpty && kubeFieldsComplete(visibleFields, values: values) }
    private var actionLabel: String {
        google ? String(localized: "Sign in with Google") :
            kubeDiscoverNeedsSignIn(fields) ? String(localized: "Sign in and find clusters") : String(localized: "Find clusters")
    }

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
                .disabled(working)
                if sets.count > 1 {
                    Section {
                        Picker("Sign in with", selection: $option) {
                            ForEach(sets.indices, id: \.self) { index in
                                Text(KubeAuthWording.optionLabel(sets[index])).tag(index)
                            }
                        }
                        .pickerStyle(.segmented)
                        .disabled(working)
                    }
                }
                if !visibleFields.isEmpty {
                    KubeFieldsSection(fields: visibleFields, values: $values).disabled(working)
                }
                let hints = fields.compactMap(KubeAuthWording.fieldHint)
                if !hints.isEmpty {
                    Section {
                        ForEach(hints, id: \.self) { Text($0).font(.footnote).foregroundStyle(.secondary) }
                    }
                }
                Section {
                    CloudDiscoverySignInRows(flow: flow, onCancel: cancelSignIn)
                    if busy { DiscoverProgressRow(progress: progress) }
                    if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
                    Button {
                        findClusters()
                    } label: {
                        HStack {
                            Text(actionLabel)
                            if working { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(working || !complete)
                } footer: {
                    if kubeDiscoverNeedsSignIn(fields) { Text("Sign in first, then find clusters.") }
                }
            }
            .navigationTitle("Add from a cloud account")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { cancelSignIn(); work?.cancel(); dismiss() }
                }
            }
        }
        .interactiveDismissDisabled(working)
        .task { await loadFields() }
        // Another provider: its own fields, nothing typed for the previous one carried over.
        .onChange(of: provider) { option = 0; resetCredentials() }
        .onChange(of: option) { resetCredentials() }
        .onChange(of: flow.phase) { _, phase in
            switch phase {
            case .signedIn:
                guard let secrets = pendingSecrets else { return }
                signedSecrets = secrets
                work = Task { await discover(secrets: secrets) }
            case .failed(let message): error = message
            case .idle: pendingSecrets = nil
            default: break
            }
        }
        .onDisappear {
            flow.cancel()
            work?.cancel()
            // A successful discovery hands its session to the import preview. Clearing it
            // here would make every imported cluster need another browser sign-in.
            if !handedOff { TalosClient.forgetDiscoverSignIn() }
        }
    }

    private func loadFields() async {
        do {
            fieldsByProvider = try await TalosClient.discoverFields()
            optionsByProvider = try await TalosClient.discoverOptions()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func findClusters() {
        guard !working && complete else { return }
        error = nil
        let secrets = google ? kubeGoogleSignInSecretsJSON(projects: values["gcpProjects"] ?? "") :
            kubeSecretsJSON(fields: fields, values: values)
        pendingSecrets = secrets
        if kubeDiscoverNeedsSignIn(fields), signedSecrets != secrets {
            flow.start(TalosClient.startDiscoverSignIn(provider: provider, secrets: secrets))
        } else {
            work = Task { await discover(secrets: secrets) }
        }
    }

    private func cancelSignIn() {
        flow.cancel()
        pendingSecrets = nil
        signedSecrets = nil
        TalosClient.forgetDiscoverSignIn()
    }

    private func resetCredentials() {
        cancelSignIn()
        values = [:]
        error = nil
    }

    private func discover(secrets: String) async {
        busy = true
        progress = nil
        let poll = Task { await pollProgress() }
        defer {
            poll.cancel()
            busy = false
        }
        let selectedProvider = provider
        do {
            let kubeconfig = try await TalosClient.discoverClusters(provider: selectedProvider, secrets: secrets)
            guard !Task.isCancelled else { return }
            handedOff = true
            onFound(DiscoveredClusters(kubeconfig: kubeconfig, secrets: secrets))
            dismiss()
        } catch {
            guard !Task.isCancelled else { return }
            self.error = error.localizedDescription
            if isKubeSignInRequired(error.localizedDescription) { signedSecrets = nil }
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
