import SwiftUI
import IchorCore
import UIKit
import UniformTypeIdentifiers

/// "Enter details": a Talos cluster typed in rather than imported, reached directly (endpoints,
/// CA, client certificate) or through Omni. The Go core builds the talosconfig, which the add
/// screen then previews and stores like an imported one.
struct TalosFormView: View {
    /// The talosconfig built, and the Omni service account key to set once it is added ("" for none).
    var onBuilt: (_ yaml: String, _ omniKey: String) -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(onBuilt: @escaping (_ yaml: String, _ omniKey: String) -> Void) {
        self.onBuilt = onBuilt
    }

    @Environment(\.dismiss) private var dismiss
    @State private var form = TalosForm()
    @State private var busy = false
    @State private var error: String?
    /// The certificate field a file is being picked for.
    @State private var importingFor: PemField?

    enum PemField: Identifiable {
        case ca, crt, key
        var id: Self { self }
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Connection", selection: $form.mode) {
                        Text(verbatim: "Talos").tag(TalosForm.Mode.direct)
                        Text(verbatim: "Omni").tag(TalosForm.Mode.omni)
                    }
                    .pickerStyle(.segmented)
                    TextField("Name", text: $form.name)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
                switch form.mode {
                case .direct: directSections
                case .omni: omniSections
                }
                Section {
                    if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
                    Button {
                        Task { await build() }
                    } label: {
                        if busy { ProgressView() } else { Text("Continue") }
                    }
                    .disabled(busy || !form.canSubmit)
                }
            }
            .navigationTitle("Enter details")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
            }
        }
        .interactiveDismissDisabled(busy)
    }

    @ViewBuilder
    private var directSections: some View {
        Section {
            addressEditor(String(localized: "Endpoints"), text: $form.endpoints)
            addressEditor(String(localized: "Nodes"), text: $form.nodes)
        } footer: {
            Text("One address per line. Without nodes, the endpoints are the nodes.")
        }
        Section {
            pemEditor(String(localized: "CA certificate"), text: $form.ca, field: .ca)
            pemEditor(String(localized: "Client certificate"), text: $form.crt, field: .crt)
            pemEditor(String(localized: "Client key"), text: $form.key, field: .key)
        } footer: {
            Text("PEM, or base64 as in a talosconfig (ca, crt, key).")
        }
        // Here, not on the form: the service account key field (Omni) has a file importer of its own.
        .fileImporter(isPresented: $importingFor.isPresent(), allowedContentTypes: [.plainText, .data, .item]) { result in
            guard let field = importingFor else { return }
            switch result {
            case .success(let url): read(url, into: field)
            case .failure(let failure): error = failure.localizedDescription
            }
        }
    }

    @ViewBuilder
    private var omniSections: some View {
        Section {
            TextField(text: $form.omniURL, prompt: Text(verbatim: "https://acme.omni.example.com")) { Text("Omni address") }
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            TextField("Cluster", text: $form.cluster)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            TextField(text: $form.identity, prompt: Text(verbatim: "ops@example.com")) { Text("Account email") }
                .keyboardType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        } footer: {
            Text("The cluster's name in Omni. The email is needed to sign in with the browser.")
        }
        Section {
            OmniKeyField(key: $form.serviceAccountKey)
        } header: {
            Text("Service account key")
        } footer: {
            Text("Optional. Without a key, sign in with the browser once the cluster is added.")
        }
    }

    private func addressEditor(_ title: String, text: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: title).font(.footnote).foregroundStyle(.secondary)
            TextEditor(text: text)
                .font(.system(.callout, design: .monospaced))
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .frame(minHeight: 60)
        }
    }

    private func pemEditor(_ title: String, text: Binding<String>, field: PemField) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: title).font(.footnote).foregroundStyle(.secondary)
            TextEditor(text: text)
                .font(.system(.caption, design: .monospaced))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .frame(minHeight: 80)
            Button { importingFor = field } label: { Label("Choose file", systemImage: "doc") }
                .buttonStyle(.borderless)
        }
    }

    private func read(_ url: URL, into field: PemField) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        // A certificate or a key is a few kB.
        guard let data = try? Data(contentsOf: url), data.count <= 64 * 1024,
              let text = String(data: data, encoding: .utf8) else {
            error = String(localized: "This file cannot be read.")
            return
        }
        switch field {
        case .ca: form.ca = text
        case .crt: form.crt = text
        case .key: form.key = text
        }
        error = nil
    }

    private func build() async {
        busy = true
        defer { busy = false }
        do {
            let yaml = try await TalosClient.buildTalosconfig(form)
            error = nil
            onBuilt(yaml, form.serviceAccountKeyToSet ?? "")
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// An Omni service account key: typed hidden, pasted, or picked from a file.
struct OmniKeyField: View {
    @Binding var key: String

    init(key: Binding<String>) {
        _key = key
    }

    @State private var importing = false
    @State private var importError: String?

    var body: some View {
        SecureField("Service account key", text: $key)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        HStack(spacing: 20) {
            Button {
                if let pasted = UIPasteboard.general.string { key = pasted.trimmingCharacters(in: .whitespacesAndNewlines) }
            } label: {
                Label("Paste", systemImage: "doc.on.clipboard")
            }
            Button { importing = true } label: { Label("Choose file", systemImage: "doc") }
        }
        .buttonStyle(.borderless)
        .fileImporter(isPresented: $importing, allowedContentTypes: [.plainText, .data, .item]) { result in
            switch result {
            case .success(let url): read(url)
            case .failure(let failure): importError = failure.localizedDescription
            }
        }
        if let importError {
            Text(importError).font(.footnote).foregroundStyle(.statusBad)
        }
    }

    private func read(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: url), data.count <= 64 * 1024,
              let text = String(data: data, encoding: .utf8) else {
            importError = String(localized: "This file cannot be read.")
            return
        }
        key = text.trimmingCharacters(in: .whitespacesAndNewlines)
        importError = nil
    }
}
