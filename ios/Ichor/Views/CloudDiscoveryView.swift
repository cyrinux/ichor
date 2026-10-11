import SwiftUI
import IchorCore

/// What cloud discovery found: a kubeconfig of the account's clusters for the import preview,
/// and the credentials that found them, to sign the added clusters in with.
struct DiscoveredClusters: Equatable {
    let kubeconfig: String
    /// Field → value JSON (see kubeSecretsJSON).
    let secrets: String
    /// The AWS regions the discovery could not read: clusters there, if any, are not listed.
    var refused: [String] = []
}

/// "Add from a cloud account" (K7): a provider, its account credentials, and the clusters
/// they reach (Go DiscoverClusters), previewed like an imported kubeconfig.
struct CloudDiscoveryView: View {
    let onFound: (DiscoveredClusters) -> Void

    /// Opens on `provider`, one of kubeDiscoverProviders.
    init(provider: String = kubeDiscoverProviders[0], initialValues: [String: String] = [:], onFound: @escaping (DiscoveredClusters) -> Void) {
        self.onFound = onFound
        _provider = State(initialValue: provider)
        _values = State(initialValue: initialValues)
        _profileRole = State(initialValue: initialValues)
        initialFields = Set(initialValues.keys)
    }

    private let initialFields: Set<String>
    /// An imported ~/.aws/config's account and role, ticked once the sign-in lists the choices.
    @State private var profileRole: [String: String]
    @State private var scanning = false
    @State private var didLoad = false

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
    /// After an IAM Identity Center sign-in with several accounts and roles: the ones offered,
    /// and those ticked to look in.
    @State private var roleChoices: [KubeSsoRoleChoice] = []
    @State private var pickedRoles: Set<KubeSsoRoleChoice> = []

    private var working: Bool { busy || flow.isRunning }
    private var google: Bool { fields.contains(kubeGoogleSignInField) }
    private var visibleFields: [String] { fields.filter { $0 != kubeGoogleSignInField && $0 != kubeGcloudSignInField } }
    private var choosing: Bool { !roleChoices.isEmpty && fields.contains(kubeAWSStartURLField) }
    private var complete: Bool {
        !fields.isEmpty && kubeFieldsComplete(visibleFields, values: values) && (!choosing || !pickedRoles.isEmpty)
    }
    private var actionLabel: String {
        google ? String(localized: "Sign in with Google") :
            kubeDiscoverNeedsSignIn(fields) && !choosing ? String(localized: "Sign in and find clusters") : String(localized: "Find clusters")
    }
    /// The hints of the fields shown; discovery's IAM Identity Center explains itself with the
    /// regions' hint, the sign-in sheet's start URL hint names fields it does not have.
    private var hints: [String] {
        fields.filter { !(fields.contains(kubeAWSRegionsField) && $0 == kubeAWSStartURLField) }.compactMap(KubeAuthWording.fieldHint)
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
                    KubeFieldsSection(fields: visibleFields, values: $values).disabled(working || choosing)
                }
                if choosing {
                    Section("Choose the accounts and roles to look in") {
                        ForEach(roleChoices, id: \.self) { choice in
                            Toggle(isOn: Binding(
                                get: { pickedRoles.contains(choice) },
                                set: { on in if on { pickedRoles.insert(choice) } else { pickedRoles.remove(choice) } }
                            )) {
                                VStack(alignment: .leading) {
                                    Text(verbatim: choice.label)
                                    Text(verbatim: choice.accountId).font(.footnote).foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                    .disabled(working)
                }
                if provider == "gke" {
                    Section {
                        Button("Scan credentials QR code") { scanning = true }.disabled(working)
                    } footer: {
                        Text("This file contains a refresh token or private key. Keep it private; anyone with it can access your Google account or service account.")
                    }
                }
                if provider == "eks" && fields.contains(kubeAWSStartURLField) && !choosing {
                    Section {
                        Button("Scan ~/.aws/config QR code") { scanning = true }.disabled(working)
                    } footer: {
                        Text("Scan your ~/.aws/config (qrencode -t ansiutf8 < ~/.aws/config). It holds no secret: only the start URL, region, account and role. The first profile that uses IAM Identity Center is taken.")
                    }
                }
                if !hints.isEmpty {
                    Section {
                        ForEach(hints, id: \.self) { Text($0).font(.footnote).foregroundStyle(.secondary) }
                    }
                }
                Section {
                    CloudDiscoverySignInRows(flow: flow, onCancel: cancelSignIn)
                    if busy { DiscoverProgressRow(progress: progress, regions: provider == "eks") }
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
                    if kubeDiscoverNeedsSignIn(fields) && !choosing { Text("Sign in first, then find clusters.") }
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
        .sheet(isPresented: $scanning) {
            NavigationStack {
                QRScannerView { text in
                    scanning = false
                    Task { await useScannedCredentials(text) }
                }
                .navigationTitle("Scan credentials QR code")
                .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { scanning = false } } }
            }
        }
        // Another provider: its own fields, nothing typed for the previous one carried over.
        .onChange(of: provider) { option = 0; resetCredentials() }
        // A scanned credential picks its option and value together: keep what fits the new option.
        .onChange(of: option) {
            guard didLoad else { return }
            cancelSignIn()
            values = values.filter { fields.contains($0.key) }
            error = nil
        }
        .onChange(of: flow.phase) { _, phase in
            switch phase {
            case .signedIn:
                guard let secrets = pendingSecrets else { return }
                signedSecrets = secrets
                // Several AWS accounts and roles: the user ticks the ones to look in first.
                let choices = provider == "eks" ? TalosClient.discoverSignInChoices() : []
                roleChoices = choices
                pickedRoles = Set(choices.filter { $0.matches(profileRole) })
                if choices.isEmpty { work = Task { await discover(secrets: secrets) } }
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
            if !didLoad, !initialFields.isEmpty { option = sets.firstIndex { !initialFields.isDisjoint(with: $0) } ?? 0 }
            didLoad = true
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func findClusters() {
        guard !working && complete else { return }
        error = nil
        let secrets = google ? kubeGoogleSignInSecretsJSON(projects: values["gcpProjects"] ?? "") :
            kubeSecretsJSON(fields: fields, values: values)
        if choosing {
            let picked = roleChoices.filter(pickedRoles.contains)
            var withRoles = values
            withRoles[kubeAWSRoleSelectionField] = kubeRoleSelection(picked)
            let selected = kubeSecretsJSON(fields: fields + [kubeAWSRoleSelectionField], values: withRoles)
            work = Task { await discover(secrets: selected) }
            return
        }
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
        roleChoices = []
        pickedRoles = []
        TalosClient.forgetDiscoverSignIn()
    }

    private func resetCredentials() {
        cancelSignIn()
        values = [:]
        profileRole = [:]
        error = nil
    }

    private func useScannedCredentials(_ text: String) async {
        do {
            let decoded = try await TalosClient.decodeImportText(text)
            let route = try await TalosClient.classifyImportText(decoded)
            if provider == "eks" {
                guard route.isEksIdentityCenter else {
                    error = String(localized: "Scan an ~/.aws/config that signs in with IAM Identity Center (an sso-session, or a profile with sso_start_url).")
                    return
                }
                option = sets.firstIndex { $0.contains(kubeAWSStartURLField) } ?? 0
                values = route.values
                profileRole = route.values
                error = nil
                return
            }
            guard route.isGkeCredential else {
                error = String(localized: "Scan a gcloud user credentials file or a service account key.")
                return
            }
            option = sets.firstIndex { $0.contains(route.field) } ?? 0
            values = [route.field: decoded]
            error = nil
        } catch { self.error = error.localizedDescription }
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
            let refused = TalosClient.discoverProgress()?.refused ?? []
            onFound(DiscoveredClusters(kubeconfig: kubeconfig, secrets: secrets, refused: refused))
            dismiss()
        } catch {
            guard !Task.isCancelled else { return }
            self.error = error.localizedDescription
            if isKubeSignInRequired(error.localizedDescription) {
                signedSecrets = nil
                roleChoices = []
            }
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

/// How far the running discovery got: a bar that fills as a Google account's projects (an AWS
/// sign-in's regions) are read (indeterminate while they are listed, and for the other clouds),
/// the counts, and the regions that refused.
private struct DiscoverProgressRow: View {
    let progress: DiscoveryProgress?
    /// Counting AWS regions rather than projects.
    let regions: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let progress, let fraction = progress.fraction {
                ProgressView(value: fraction)
                Group {
                    if regions {
                        Text("Scanned \(String(progress.scanned)) of \(String(progress.projects)) regions · \(String(progress.clusters)) clusters found")
                    } else {
                        Text("Scanned \(String(progress.scanned)) of \(String(progress.projects)) projects · \(String(progress.clusters)) clusters found")
                    }
                }
                .font(.footnote).foregroundStyle(.secondary)
                .monospacedDigit()
            } else {
                ProgressView().frame(maxWidth: .infinity, alignment: .leading)
                Text("Looking for clusters…").font(.footnote).foregroundStyle(.secondary)
            }
            if let refused = progress?.refused, !refused.isEmpty {
                Text("No access in \(refused.joined(separator: ", "))").font(.footnote).foregroundStyle(.secondary)
            }
        }
        .animation(.default, value: progress)
    }
}
