import SwiftUI
import IchorCore

/// "Add clusters from Sidero Omni": the instance's URL, then an account (it confirms a key in
/// the browser) or a service account key. The account's clusters come back as a talosconfig
/// for the import preview, already signed in (Go DiscoverOmniClusters).
struct OmniDiscoveryView: View {
    let onFound: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var flow = KubeSignInFlow()
    @State private var serviceAccount = false
    @State private var url = ""
    @State private var email = ""
    @State private var key = ""
    @State private var busy = false
    @State private var error: String?

    private var endpoint: String { url.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var working: Bool { busy || flow.isRunning }
    private var complete: Bool {
        !endpoint.isEmpty && !(serviceAccount ? key : email).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Sign in", selection: $serviceAccount) {
                        Text("Account").tag(false)
                        Text("Omni service account").tag(true)
                    }
                    .pickerStyle(.segmented)
                } footer: {
                    Text("Sign in to your Omni instance: its clusters show in the preview, already signed in, Kubernetes included.")
                }
                Section {
                    TextField("Omni URL (https://…omni.siderolabs.io)", text: $url)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    if serviceAccount {
                        SecureField("Service account key (OMNI_SERVICE_ACCOUNT_KEY)", text: $key)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    } else {
                        TextField("Account email", text: $email)
                            .keyboardType(.emailAddress)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    }
                }
                .disabled(working)
                Section {
                    if flow.phase == .browser {
                        HStack(spacing: 12) {
                            ProgressView()
                            Text("Waiting for the sign-in in the browser…")
                        }
                    }
                    if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
                    Button {
                        signIn()
                    } label: {
                        HStack {
                            Text("Sign in")
                            if working { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(working || !complete)
                }
            }
            .navigationTitle("Add clusters from Sidero Omni")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) {
                        flow.cancel()
                        dismiss()
                    }
                }
            }
        }
        .interactiveDismissDisabled(working)
        .onChange(of: flow.phase) { _, phase in
            switch phase {
            case .signedIn: Task { await discover(identity: email.trimmingCharacters(in: .whitespacesAndNewlines)) }
            case .failed(let message): error = message
            default: break
            }
        }
        .onChange(of: serviceAccount) { error = nil }
    }

    private func signIn() {
        error = nil
        if serviceAccount {
            busy = true
            Task {
                defer { busy = false }
                do {
                    let identity = try await TalosClient.omniServiceAccount(endpoint: endpoint, key: key.trimmingCharacters(in: .whitespacesAndNewlines))
                    await discover(identity: identity)
                } catch {
                    self.error = error.localizedDescription
                }
            }
        } else {
            flow.start(TalosClient.startOmniSignIn(endpoint: endpoint, email: email.trimmingCharacters(in: .whitespacesAndNewlines)))
        }
    }

    private func discover(identity: String) async {
        busy = true
        defer { busy = false }
        do {
            let talosconfig = try await TalosClient.discoverOmni(endpoint: endpoint, identity: identity)
            dismiss()
            onFound(talosconfig)
        } catch {
            self.error = error.localizedDescription
        }
    }
}
