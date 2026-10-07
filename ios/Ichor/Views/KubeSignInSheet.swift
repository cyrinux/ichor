import SwiftUI
import IchorCore
import UIKit
import UniformTypeIdentifiers

/// Signs a kubeconfig cluster in: who it is signed in as and until when, and the way in its
/// method takes (credentials to enter, the browser, or a device code). Successes reload the
/// cluster's screens.
struct KubeSignInSheet: View {
    let target: KubeSignInTarget
    var onSignedIn: () -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(target: KubeSignInTarget, onSignedIn: @escaping () -> Void = {}) {
        self.target = target
        self.onSignedIn = onSignedIn
    }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var flow = KubeSignInFlow()
    @State private var info: KubeSignInInfo?
    @State private var loadError: String?
    @State private var loading = true
    @State private var signingOut = false

    var body: some View {
        NavigationStack {
            Form {
                statusSection
                KubeSignInProgress(flow: flow, target: target)
                if let info {
                    if info.isCredentials {
                        KubeCredentialsForm(info: info, busy: flow.isRunning) { secrets in
                            Task { await flow.signIn(target, secrets: secrets) }
                        }
                    } else {
                        Section {
                            Button("Sign in") { flow.start(target) }
                                .disabled(flow.isRunning)
                        }
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
        .task { await loadInfo() }
        .onChange(of: flow.phase) { _, phase in
            guard phase == .signedIn else { return }
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
                LabeledContent("Sign-in", value: KubeAuthWording.methodLabel(info.method))
                if info.signedIn {
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
            info = try await TalosClient.signInInfo(kube: target.kube, context: target.context, talos: target.talos)
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
            try await TalosClient.signOut(kube: target.kube, context: target.context, talos: target.talos)
            model.reloadKubernetes()
            await loadInfo()
        } catch {
            loadError = error.localizedDescription
        }
    }
}

/// Where an interactive sign-in is: waiting for the browser, a device code to enter, done, or why it failed.
private struct KubeSignInProgress: View {
    let flow: KubeSignInFlow
    let target: KubeSignInTarget

    @Environment(\.openURL) private var openURL

    var body: some View {
        switch flow.phase {
        case .idle:
            EmptyView()
        case .working:
            Section { ProgressView() }
        case .browser:
            Section {
                HStack(spacing: 12) {
                    ProgressView()
                    Text("Waiting for the sign-in in the browser…")
                }
                Button("Cancel", role: .cancel) { flow.cancel() }
            }
        case .device(let prompt):
            deviceSection(prompt)
        case .signedIn:
            Section {
                Label("Signed in.", systemImage: "checkmark.circle.fill").foregroundStyle(.statusOK)
            }
        case .failed(let message):
            Section {
                Text(message).font(.footnote).foregroundStyle(.statusBad)
            }
        }
    }

    private func deviceSection(_ prompt: KubeSignInPrompt) -> some View {
        Section {
            Text("Enter this code on the sign-in page:")
            Text(verbatim: prompt.userCode ?? "")
                .font(.system(.largeTitle, design: .monospaced).weight(.semibold))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity)
            Button {
                UIPasteboard.general.string = prompt.userCode
            } label: {
                Label("Copy code", systemImage: "doc.on.doc")
            }
            if let url = URL(string: prompt.openURL) {
                Button {
                    openURL(url)
                } label: {
                    Label("Open the sign-in page", systemImage: "safari")
                }
            }
            HStack(spacing: 12) {
                ProgressView()
                Text("Waiting for you to approve the sign-in…").font(.footnote).foregroundStyle(.secondary)
            }
            Button("Cancel", role: .cancel) { flow.cancel() }
        } footer: {
            if prompt.expiresIn > 0 {
                Text("The code expires in \(localizedDuration(Int64(prompt.expiresIn))).")
            }
        }
    }
}

/// The fields of a credentials method: the option to use (EKS: IAM Identity Center or access
/// keys), then each field, secrets hidden, a service account key pasted or picked from a file.
struct KubeCredentialsForm: View {
    let info: KubeSignInInfo
    let busy: Bool
    let onSubmit: (String) -> Void

    init(info: KubeSignInInfo, busy: Bool, onSubmit: @escaping (String) -> Void) {
        self.info = info
        self.busy = busy
        self.onSubmit = onSubmit
    }

    @State private var option = 0
    @State private var values: [String: String] = [:]

    private var fields: [String] {
        let sets = info.fieldSets
        return sets.indices.contains(option) ? sets[option] : []
    }

    var body: some View {
        if info.fieldSets.count > 1 {
            Section {
                Picker("Sign in with", selection: $option) {
                    ForEach(info.fieldSets.indices, id: \.self) { index in
                        Text(KubeAuthWording.optionLabel(info.fieldSets[index])).tag(index)
                    }
                }
                .pickerStyle(.segmented)
            }
        }
        KubeFieldsSection(fields: fields, values: $values)
        Section {
            Button("Save and sign in") { onSubmit(kubeSecretsJSON(fields: fields, values: values)) }
                .disabled(busy || !kubeFieldsComplete(fields, values: values))
        } footer: {
            if fields.contains(where: { kubeFieldInput($0) == .secret || kubeFieldInput($0) == .json }) {
                Text("Stored sealed on this device, and in your encrypted backups. Prefer a dedicated identity with read-only access.")
            }
        }
    }
}

/// Text fields for Go field names (sign-in or cloud discovery), with localized labels.
struct KubeFieldsSection: View {
    let fields: [String]
    @Binding var values: [String: String]

    init(fields: [String], values: Binding<[String: String]>) {
        self.fields = fields
        _values = values
    }

    @State private var importingFor: String?
    @State private var importError: String?

    var body: some View {
        Section {
            ForEach(fields, id: \.self) { field in
                fieldView(field)
            }
            if let importError {
                Text(importError).font(.footnote).foregroundStyle(.statusBad)
            }
        }
        .fileImporter(isPresented: $importingFor.isPresent(), allowedContentTypes: [.json, .plainText, .data]) { result in
            guard let field = importingFor else { return }
            switch result {
            case .success(let url): read(url, into: field)
            case .failure(let failure): importError = failure.localizedDescription
            }
        }
    }

    @ViewBuilder
    private func fieldView(_ field: String) -> some View {
        let label = KubeAuthWording.fieldLabel(field)
        let prompt = KubeAuthWording.fieldPrompt(field).map { Text(verbatim: $0) }
        switch kubeFieldInput(field) {
        case .secret:
            SecureField(text: binding(field), prompt: prompt) { Text(label) }
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        case .plain:
            TextField(text: binding(field), prompt: prompt) { Text(label) }
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(field.hasSuffix("Url") || field == "rancherServer" ? .URL : .default)
        case .json:
            VStack(alignment: .leading, spacing: 6) {
                Text(label).font(.footnote).foregroundStyle(.secondary)
                TextEditor(text: binding(field))
                    .font(.system(.caption, design: .monospaced))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .frame(minHeight: 100)
                Button { importingFor = field } label: { Label("Choose file", systemImage: "doc") }
                    .buttonStyle(.borderless)
            }
        }
    }

    private func binding(_ field: String) -> Binding<String> {
        Binding(get: { values[field] ?? "" }, set: { values[field] = $0 })
    }

    private func read(_ url: URL, into field: String) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        // A service account key is a few kB.
        guard let data = try? Data(contentsOf: url), data.count <= 64 * 1024,
              let text = String(data: data, encoding: .utf8) else {
            importError = String(localized: "This file cannot be read.")
            return
        }
        values[field] = text
        importError = nil
    }
}
