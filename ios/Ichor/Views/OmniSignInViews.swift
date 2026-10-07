import SwiftUI
import IchorCore

/// Signs a Talos cluster reached through Omni in: who its requests are signed as and until
/// when, then a service account key to enter or the browser. Successes reload the screens.
struct OmniSignInSheet: View {
    let target: OmniSignInTarget
    /// Starts the browser sign-in as soon as the sheet is up.
    var browser: Bool
    var onSignedIn: () -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(target: OmniSignInTarget, browser: Bool = false, onSignedIn: @escaping () -> Void = {}) {
        self.target = target
        self.browser = browser
        self.onSignedIn = onSignedIn
    }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var flow = KubeSignInFlow()
    @State private var info: OmniSignInInfo?
    @State private var loadError: String?
    @State private var loading = true
    @State private var signingOut = false
    @State private var key = ""

    var body: some View {
        NavigationStack {
            Form {
                statusSection
                KubeSignInProgress(flow: flow)
                Section {
                    OmniKeyField(key: $key)
                    Button("Save key") { Task { await flow.setServiceAccount(target, key: key) } }
                        .disabled(flow.isRunning || key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                } header: {
                    Text("Service account key")
                } footer: {
                    Text("Created in Omni under Settings, Service Accounts. Stored sealed on this device, and in your encrypted backups.")
                }
                Section {
                    Button("Sign in with browser") { flow.startOmni(target) }
                        .disabled(flow.isRunning || !target.canSignInWithBrowser)
                } footer: {
                    if target.canSignInWithBrowser {
                        Text("The key approved in the browser stays on this device and lasts a few hours.")
                    } else {
                        Text("Signing in with the browser needs the account's email in the cluster's config.")
                    }
                }
            }
            .navigationTitle(Text("Sign in to \(target.label)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") {
                        flow.cancel()
                        dismiss()
                    }
                }
            }
        }
        .interactiveDismissDisabled(flow.isRunning)
        .task {
            await loadInfo()
            if browser && target.canSignInWithBrowser { flow.startOmni(target) }
        }
        .onChange(of: flow.phase) { _, phase in
            guard phase == .signedIn else { return }
            key = ""
            model.reloadKubernetes()
            onSignedIn()
            Task { await loadInfo() }
        }
        .onDisappear { flow.cancel() }
    }

    @ViewBuilder
    private var statusSection: some View {
        Section {
            if loading {
                ProgressView()
            } else if let loadError {
                Text(loadError).font(.footnote).foregroundStyle(.statusBad)
            } else if let info {
                if !info.instance.isEmpty { LabeledContent("Omni address", value: info.instance) }
                if info.signedIn {
                    LabeledContent("Sign-in", value: info.isServiceAccount
                                   ? String(localized: "Service account key") : String(localized: "Browser"))
                    if let user = info.user { LabeledContent("Signed in as", value: user) }
                    if info.sessionExpires > 0 {
                        LabeledContent("Session ends", value: Date(timeIntervalSince1970: TimeInterval(info.sessionExpires))
                            .formatted(date: .abbreviated, time: .shortened))
                    }
                    Button("Sign out", role: .destructive) { Task { await signOut() } }
                        .disabled(signingOut || flow.isRunning)
                } else {
                    Text("Not signed in").foregroundStyle(.secondary)
                }
            }
        } header: {
            Text(verbatim: target.label)
        }
    }

    private func loadInfo() async {
        do {
            info = try await TalosClient.omniSignInInfo(config: target.config, context: target.context)
            loadError = nil
        } catch {
            loadError = error.localizedDescription
        }
        loading = false
    }

    private func signOut() async {
        signingOut = true
        defer { signingOut = false }
        do {
            try await TalosClient.omniSignOut(config: target.config, context: target.context)
            model.reloadKubernetes()
            await loadInfo()
        } catch {
            loadError = error.localizedDescription
        }
    }
}

/// How the Omni sign-in sheet opens: on the key, or with the browser sign-in started.
private struct OmniSignInRequest: Identifiable {
    let target: OmniSignInTarget
    let browser: Bool
    var id: String { target.id + (browser ? "|browser" : "") }
}

/// What a screen shows instead of its data when its load failed for want of an Omni key.
struct OmniSignInRequiredView: View {
    let target: OmniSignInTarget
    let reason: String
    let retry: () async -> Void

    init(target: OmniSignInTarget, reason: String, retry: @escaping () async -> Void) {
        self.target = target
        self.reason = reason
        self.retry = retry
    }

    @State private var request: OmniSignInRequest?

    var body: some View {
        ContentUnavailableView {
            Label("Sign-in needed", systemImage: "person.badge.key")
        } description: {
            Text(reason.isEmpty ? String(localized: "Sign in to Omni to use this cluster.") : reason)
        } actions: {
            Button("Enter service account key") { request = OmniSignInRequest(target: target, browser: false) }
                .buttonStyle(.borderedProminent)
            if target.canSignInWithBrowser {
                Button("Sign in with browser") { request = OmniSignInRequest(target: target, browser: true) }
                    .buttonStyle(.bordered)
            }
        }
        .sheet(item: $request) { request in
            OmniSignInSheet(target: request.target, browser: request.browser) { Task { await retry() } }
        }
    }
}

/// The Omni sign-in of the cluster on screen, in the settings: who its requests are signed
/// as, with the way to the sign-in sheet.
struct OmniSignInSection: View {
    let target: OmniSignInTarget

    init(target: OmniSignInTarget) {
        self.target = target
    }

    @Environment(AppModel.self) private var model
    @State private var info: OmniSignInInfo?
    @State private var error: String?
    @State private var signingIn: OmniSignInTarget?

    var body: some View {
        Section {
            if let info {
                if info.signedIn, let user = info.user {
                    LabeledContent("Signed in as", value: user)
                } else if !info.signedIn {
                    Text("Not signed in").foregroundStyle(.secondary)
                }
            } else if let error {
                Text(error).font(.footnote).foregroundStyle(.statusBad)
            }
            Button("Sign in") { signingIn = target }
        } header: {
            Text(verbatim: "Omni")
        }
        .sheet(item: $signingIn) { OmniSignInSheet(target: $0) }
        .task(id: "\(target.id)#\(model.dataGeneration)") { await load() }
    }

    private func load() async {
        do {
            info = try await TalosClient.omniSignInInfo(config: target.config, context: target.context)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}
